package main

import (
	"context"
	"cpeos/agentLog"
	"cpeos/app"
	"cpeos/config"
	"cpeos/public"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"gitlab.daho.tech/gdaho/etcd"
	"gitlab.daho.tech/gdaho/log"
	"gitlab.daho.tech/gdaho/network"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type VapStatusToCore struct {
	Status string `json:"status"` /* UP 或者 DOWN */
	Tsms   int64  `json:"ts"`
}

type VapCheckConf struct {
	Type     int
	PingType int
	ObjType  int
	Target   string
	Source   string
}

type VapStatus struct {
	Namespace string `json:"namespace"`
	Id        string `json:"id"`
	Tsms      string `json:"tsms"`
	Status    string `json:"status"`
}

type VapStatusCtl struct {
	VapStatus
	Conf      VapCheckConf
	DownCnt   int
	HoldCnt   int
	AgeFlag   bool
	StatusChg bool
	ChgTime   time.Time
}

type DelayResult struct {
	VapId     string
	Delay     float64
	StartTime time.Time
}

const (
	DOWNCNT_MAX       = 3
	TIMEOUT           = 1000
	VapDetectInterval = 2
	VapRetransCount   = 3000
	ProtocolICMP      = 1
	fakeLatency       = 0xFFFF
	EtcdServer        = "127.0.0.1:2379"

	/*  Vap check Type: 检查类型 */
	VapCheckType_Ipsec = 0
	VapCheckType_Gre   = 1
	VapCheckType_Port  = 2

	/* Vap Obj Tyoe：实例类型 */
	VapObjType_Port = 0
	VapObjType_Vap  = 1
	VapObjType_Conn = 2
	VapObjType_Link = 3
	VapObjType_Tunn = 4

	/* PingType : Ping方式 */
	PingType_NoPing    = 0
	PingType_PingInNs  = 1
	PingType_PingOutNs = 2
)

var (
	EtcdClient         *etcd.Client
	swanctl_cmd        = "swanctl"
	swanctl_cmd_listsa = "--list-sa --ike %s --noblock"
	logName            = "/var/log/cpe/monitor.log"
	G_VapStatusCtl     = make(map[string]*VapStatusCtl)
	Up                 = 1
	Down               = 0
	G_SmoothFlag       = true
	monitorpath        = "/var/run/monitor/"
	rootStatusPath     = "/root/active"
)

func updateStatusToFile(status, filepath string) error {

	if status == public.VAPNORMAL {
		if !public.FileExists(filepath) {
			fileConf, err := os.OpenFile(filepath, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
			if err != nil && !os.IsExist(err) {
				agentLog.AgentLogger.Info("updateStatusToFile OpenFile fail: ", filepath, err)
				return err
			}
			defer fileConf.Close()
			agentLog.AgentLogger.Info("updateStatusToFile change to Up: ", filepath)
		}
	} else {
		if public.FileExists(filepath) {
			err := os.Remove(filepath)
			if err != nil {
				agentLog.AgentLogger.Info("updateStatusToFile Remove fail: ", filepath, err)
				return err
			}
			agentLog.AgentLogger.Info("updateStatusToFile change to Down: ", filepath)
		}
	}

	return nil
}

func updateChangedToFile(filepath string) error {

	if !public.FileExists(filepath) {
		fileConf, err := os.OpenFile(filepath, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
		if err != nil && !os.IsExist(err) {
			agentLog.AgentLogger.Info("updateChangedToFile OpenFile fail: ", filepath, err)
			return err
		}
		defer fileConf.Close()
		agentLog.AgentLogger.Info("updateChangedToFile has changed: ", filepath)
	}

	return nil
}

func updateStatusNoCheck(resStatus *DelayResult, vapStatus *VapStatusCtl) {
	StatusChg := false
	vapStatus.HoldCnt++
	if resStatus.Delay == 0 {
		vapStatus.DownCnt++
		if resStatus.Delay == 0 && vapStatus.Status != public.VAPOFFLINE && vapStatus.DownCnt >= DOWNCNT_MAX {
			vapStatus.Status = public.VAPOFFLINE
			vapStatus.StatusChg = true
			StatusChg = true
			vapStatus.ChgTime = resStatus.StartTime
			agentLog.AgentLogger.Info("Monitor [", vapStatus.Id, "] Status Change to Down.")
		}
	} else {
		vapStatus.DownCnt = 0
		if resStatus.Delay != 0 && vapStatus.Status != public.VAPNORMAL {
			vapStatus.Status = public.VAPNORMAL
			vapStatus.StatusChg = true
			StatusChg = true
			vapStatus.ChgTime = resStatus.StartTime
			agentLog.AgentLogger.Info("Monitor [", vapStatus.Id, "] Status Change to Up.")
		}
	}

	if StatusChg {
		vapStatus.HoldCnt = 0
		updateChangedToFile(monitorpath + resStatus.VapId + ".changed")
	}

	updateStatusToFile(vapStatus.Status, monitorpath+resStatus.VapId)
}

/*
port check
*/
func checkPortStatus(vap *VapStatusCtl, chs chan DelayResult) {
	//port status
	/* 如果port没有开启healthcheck，则在Namespace中填充LogicPortName */
	cmdstr := fmt.Sprintf(`ifconfig %s | grep "flags"`, vap.Namespace)
	err, result_str := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("IfconfigStatusDetect:", cmdstr, err)
	} else {
		if strings.Contains(result_str, vap.Namespace) &&
			strings.Contains(result_str, "RUNNING") &&
			strings.Contains(result_str, "UP") {
			// 写入一个非0时延，模拟up状态
			chs <- DelayResult{VapId: vap.Id, Delay: fakeLatency, StartTime: time.Now()}
			return
		}
	}
	chs <- DelayResult{VapId: vap.Id, Delay: 0, StartTime: time.Now()}
}

/*
gre check
*/
func checkGreStatus(vap *VapStatusCtl, chs chan DelayResult) {
	//gre status
	var cmdstr string
	if vap.Namespace == "" {
		cmdstr = fmt.Sprintf(`ifconfig %s | grep "flags"`, vap.Id)
	} else {
		cmdstr = fmt.Sprintf(`ip netns exec %s ifconfig %s | grep "flags"`, vap.Namespace, vap.Id)
	}
	err, result_str := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("IfconfigStatusDetect:", cmdstr, err)
	} else {
		if strings.Contains(result_str, vap.Id) &&
			strings.Contains(result_str, "RUNNING") &&
			strings.Contains(result_str, "UP") {
			// 写入一个非0时延，模拟up状态
			chs <- DelayResult{VapId: vap.Id, Delay: fakeLatency, StartTime: time.Now()}
			return
		}
	}
	chs <- DelayResult{VapId: vap.Id, Delay: 0, StartTime: time.Now()}
}

/*
ipsec vpn check
*/
func checkIpsecStatus(vap *VapStatusCtl, chs chan DelayResult) {
	//ipsec tunnel
	para := strings.Split(fmt.Sprintf(swanctl_cmd_listsa, vap.Id), " ")
	if err, ret := public.ExecCmdWithRet(swanctl_cmd, para...); err != nil {
		agentLog.AgentLogger.Info("[ERROR]swanctl --list-sa exec err: %v", err)
	} else {
		if strings.Contains(ret, vap.Id) &&
			strings.Contains(ret, "ESTABLISHED") &&
			strings.Contains(ret, "INSTALLED") {
			// 写入一个非0时延，模拟up状态
			chs <- DelayResult{VapId: vap.Id, Delay: fakeLatency, StartTime: time.Now()}
			return
		}
	}
	chs <- DelayResult{VapId: vap.Id, Delay: 0, StartTime: time.Now()}
}

/*
ping check
*/
func checkPingStatus(vap *VapStatusCtl, timeout int, chs chan DelayResult) {
	t_start := time.Now()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	vapId := vap.Id
	targetIP := vap.Conf.Target
	if strings.Contains(targetIP, "/") {
		targetIP = strings.Split(targetIP, "/")[0]
	}

	if vap.Conf.PingType == PingType_PingInNs {
		if err := network.SwitchNS(vap.Namespace); err != nil {
			agentLog.AgentLogger.Debug(fmt.Sprintf("Switch ns to %v err: %v", vapId, err))
			chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
			return
		}
		defer network.SwitchOriginNS()
	}

	listenAddr := vap.Conf.Source
	if strings.Contains(listenAddr, "/") {
		listenAddr = strings.Split(listenAddr, "/")[0]
	}

	c, err := icmp.ListenPacket("ip4:icmp", listenAddr)
	if err != nil {
		agentLog.AgentLogger.Debug(fmt.Sprintf("icmp listen for %v err: %v", vapId, err))
		chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
		return
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Duration(timeout) * time.Millisecond))

	rand.Seed(time.Now().UnixNano())
	seq := rand.Intn(1000)

	echoMsg := icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{
			ID: os.Getpid() & 0xffff, Seq: seq,
			Data: []byte("HELLO-R-U-THERE"),
		},
	}

	emsg, err := echoMsg.Marshal(nil)
	if err != nil {
		agentLog.AgentLogger.Debug(fmt.Sprintf("marshal icmp message for %v err: %v", vapId, err))
		chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
		return
	}

	if _, err := c.WriteTo(emsg, &net.IPAddr{IP: net.ParseIP(targetIP)}); err != nil {
		agentLog.AgentLogger.Debug(fmt.Sprintf("write icmp request for %v to %v err: %v", vapId, targetIP, err))
		chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
		return
	}

	rb := make([]byte, 1500)
	timeOutTimer := time.NewTimer(time.Duration(timeout) * time.Millisecond)

	for {
		select {
		case <-timeOutTimer.C:
			//agentLog.AgentLogger.Debug(fmt.Sprintf("read icmp request for %v from %v timeout", vapId, targetIP))
			chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
			return
		default:
		}

		n, peer, err := c.ReadFrom(rb)
		if err != nil {

			//agentLog.AgentLogger.Debug(fmt.Sprintf("read icmp reply for %v from %v err: %v\n", vapId, targetIP, err))
			chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
			return
		}
		t_end := time.Now()

		rMsg, err := icmp.ParseMessage(ProtocolICMP, rb[:n])
		if err != nil {
			// log.Println(err, vapId, targetIP)
			// log.Error("parse icmp reply for %v from %v err: %v", vapId, targetIP, err)
			// chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
			// return
			continue
		}

		if 0 == strings.Compare(targetIP, peer.String()) {
			if rMsg.Type == ipv4.ICMPTypeEchoReply {
				reply, ok := (rMsg.Body).(*icmp.Echo)
				if !ok {
					continue
				}
				if reply.Seq != seq {
					continue
				}
				delay := t_end.Sub(t_start).Seconds() * 1000 // ms
				chs <- DelayResult{VapId: vapId, Delay: delay, StartTime: t_start}
				return
			} else {
				// err = errors.New("icmp reply type error")
				// log.Println(err, vapId, targetIP)
				// log.Error("parse icmp reply type for %v from %v err", vapId, targetIP)
				// chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
				// return
				continue
			}
		} else {
			// err = errors.New("read from ip diff targetip")
			// log.Println(err, peer.String(), targetIP)
			// log.Error("read from ip diff targetip for %v, peer %v, target %v", vapId, peer, targetIP)
			// chs <- DelayResult{VapId: vapId, Delay: 0, StartTime: t_start}
			// return
			continue
		}
	}
}

func checkVapStatus(vap *VapStatusCtl, chs chan DelayResult) {

	switch vap.Conf.Type {
	case VapCheckType_Ipsec:
		if vap.Conf.PingType != PingType_NoPing {
			checkPingStatus(vap, TIMEOUT, chs)
		} else {
			checkIpsecStatus(vap, chs)
		}
	case VapCheckType_Gre:
		if vap.Conf.PingType != PingType_NoPing {
			checkPingStatus(vap, TIMEOUT, chs)
		} else {
			checkGreStatus(vap, chs)
		}
	case VapCheckType_Port:
		if vap.Conf.PingType != PingType_NoPing {
			checkPingStatus(vap, TIMEOUT, chs)
		} else {
			checkPortStatus(vap, chs)
		}
	default:
	}
}

func ageVapStart() {
	for _, v := range G_VapStatusCtl {
		v.AgeFlag = true
	}
}

func ageVapEnd() bool {
	aged := false
	//log.Info("age end.")
	for vapId, vapS := range G_VapStatusCtl {
		if vapS.AgeFlag {
			updateStatusToFile(public.VAPOFFLINE, monitorpath+vapId)
			delete(G_VapStatusCtl, vapId)
			log.Info("delete vap %v.", vapId)
			aged = true
		}
	}
	return aged
}

func updateVapStatus(resultList []DelayResult) {

	/* Set Root default status */
	rootStatus := public.VAPNORMAL
	for _, ins := range resultList {
		if _, ok := G_VapStatusCtl[ins.VapId]; ok {
			vapStatusCtl := G_VapStatusCtl[ins.VapId]
			///agentLog.AgentLogger.Info("updateVapStatus: VapId: ", ins.VapId, " Delay:", ins.Delay, " Status:", vapStatusCtl.Status, " Type:", vapStatusCtl.Conf.Type, " PingType:", vapStatusCtl.Conf.PingType)
			updateStatusNoCheck(&ins, vapStatusCtl)
			if vapStatusCtl.Conf.ObjType == VapObjType_Port && public.VAPNORMAL != vapStatusCtl.Status {
				/* Root 状态只关联port状态 */
				rootStatus = public.VAPOFFLINE
			}
		}
	}

	/* Update Root status */
	updateStatusToFile(rootStatus, rootStatusPath)
}

func changeEnd() {
	//for _, vapS := range G_VapStatusCtl {
	//	vapS.StatusChg = false
	//}

	if G_SmoothFlag {
		G_SmoothFlag = false
	}
}

func updateStatus2InfluxDbAndCore() (error, bool) {

	var chg = false
	var url string

	for _, vapS := range G_VapStatusCtl {
		if vapS.StatusChg || vapS.HoldCnt >= VapRetransCount {
			chg = true
			vapS.HoldCnt = 0
			var vapStatusToCore VapStatusToCore
			vapStatusToCore.Tsms = vapS.ChgTime.UnixNano() //fmt.Sprintf("%d", vapS.ChgTime.UnixNano())
			if vapS.Status == public.VAPNORMAL {
				vapStatusToCore.Status = "UP"
			} else {
				vapStatusToCore.Status = "DOWN"
			}

			bytedata, err := json.Marshal(vapStatusToCore)
			agentLog.AgentLogger.Info("Request Core data, ObjType:", vapS.Conf.ObjType, ", Id:", vapS.Id, ", Info:", string(bytedata[:]))
			if err != nil {
				agentLog.AgentLogger.Info("[ERROR]Marshal post edge err:", err)
				return errors.New("Marshal post edge msg err:" + err.Error()), false
			}

			switch vapS.Conf.ObjType {
			case VapObjType_Port:
				url = fmt.Sprintf("/api/cpeConfig/cpes/%s/logicPorts/%s/status", public.G_coreConf.Sn, vapS.Id)
			default:
				url = ""
			}

			if url != "" {
				_, err = public.RequestCore(bytedata, public.G_coreConf.CoreAddress, public.G_coreConf.CorePort, public.G_coreConf.CoreProto, url)
				if err != nil {
					agentLog.AgentLogger.Info("Request Core error, ", vapS.Conf.ObjType, ", Id:", vapS.Id)
					///return errors.New("request Core error: " + err.Error()), false
				} else {
					vapS.StatusChg = false
				}
			} else {
				vapS.StatusChg = false
			}
		}
	}

	if chg {
		changeEnd()
		return nil, true
	}

	return nil, false
}

func setVapStatus2Etcd() error {
	vapNews := make([]VapStatus, 0)
	for _, vapSCtl := range G_VapStatusCtl {
		vapNews = append(vapNews, vapSCtl.VapStatus)
	}

	//save vap status to etcd
	byteData, err := json.Marshal(vapNews)
	if err != nil {
		return err
	}

	err = EtcdClient.SetValue(config.MoniStatusPath, string(byteData[:]))
	if err != nil {
		return err
	}
	return nil
}

func getEtcdInfoMap(etcdClient *etcd.Client) (map[string]*VapStatusCtl, error) {

	etcdVapMap := make(map[string]*VapStatusCtl)

	timeStr := fmt.Sprintf("%d", time.Now().UnixNano())

	// port
	paths := []string{config.PortConfPath}
	ports, _ := etcdClient.GetValues(paths)
	for _, v := range ports {
		ins := &app.PortConf{}
		err := json.Unmarshal([]byte(v), ins)
		if err == nil {
			pingType := PingType_NoPing
			if ins.Nexthop != "" {
				pingType = PingType_PingOutNs
			}
			etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: ins.PhyifName, Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
				VapCheckConf{VapCheckType_Port, pingType, VapObjType_Port, ins.Nexthop, ins.IpAddr},
				0, 0, false, false, time.Now()}
		}
	}

	// conn
	paths = []string{config.ConnConfPath}
	conns, _ := etcdClient.GetValues(paths)
	for _, v := range conns {
		ins := &app.ConnConf{}
		err := json.Unmarshal([]byte(v), ins)
		if err == nil {
			pingType := PingType_NoPing
			if ins.Type == app.ConnType_Ipsec {
				if ins.IpsecInfo.HealthCheck {
					pingType = PingType_PingOutNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: "", Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Ipsec, pingType, VapObjType_Conn, ins.IpsecInfo.RemoteAddress, ins.IpsecInfo.LocalAddress},
					0, 0, false, false, time.Now()}
			} else if ins.Type == app.ConnType_Ssl {
				if ins.SslInfo.HealthCheck {
					pingType = PingType_PingOutNs
				}
				etcdVapMap[ins.Id] = &VapStatusCtl{VapStatus{Namespace: "", Id: ins.Id, Status: public.VAPUNKNOWN, Tsms: timeStr},
					VapCheckConf{VapCheckType_Gre, pingType, VapObjType_Conn, ins.SslInfo.RemoteAddress, ins.SslInfo.LocalAddress},
					0, 0, false, false, time.Now()}
			}
		}
	}

	return etcdVapMap, nil
}

func startStatusDetect() {

	// 从etch获取最新配置
	etcdVapMap, _ := getEtcdInfoMap(EtcdClient)

	// 老化开始，置老化标记
	ageVapStart()

	num := len(etcdVapMap)
	chs := make(chan DelayResult)
	resultList := make([]DelayResult, num)
	for _, vap := range etcdVapMap {
		vapStatus, ok := G_VapStatusCtl[vap.Id]
		if !ok {
			G_VapStatusCtl[vap.Id] = vap
		} else {
			//如果已经存在，则同步配置
			G_VapStatusCtl[vap.Id].Conf = vap.Conf
			G_VapStatusCtl[vap.Id].Namespace = vap.Namespace
		}
		G_VapStatusCtl[vap.Id].AgeFlag = false

		// 进程重启，所有抑制状态的vap状态重新上报
		if G_SmoothFlag && ok && vapStatus.Status != public.VAPUNKNOWN {
			G_VapStatusCtl[vap.Id].StatusChg = true
		}

		go checkVapStatus(vap, chs)
	}

	// 老化结束，没有更新标记的全部老化
	ageChg := ageVapEnd()

	// 抓取返回结果
	for i := 0; i < num; i++ {
		res := <-chs
		resultList[i] = res
	}

	// 结果处理
	updateVapStatus(resultList)
	err, chg := updateStatus2InfluxDbAndCore()
	if err == nil && (ageChg || chg) {
		err := setVapStatus2Etcd()
		if err != nil {
			agentLog.AgentLogger.Debug("set vap status to etcd err: %v", err)
		}
	}
}

func getVapHistoryStatus(etcdClient *etcd.Client) error {

	/* Get history status */
	vapStatus := make([]VapStatus, 0)
	etcdStatus, err := etcdClient.GetValue(config.MoniStatusPath)
	if err != nil {
		errCode := err.Error()
		if !strings.EqualFold("100", strings.Split(errCode, ":")[0]) {
			return err
		}
	} else {
		if err := json.Unmarshal([]byte(etcdStatus), &vapStatus); err != nil {
			return err
		}

		for _, v := range vapStatus {
			vapctl := &VapStatusCtl{VapStatus{Namespace: v.Namespace, Id: v.Id, Status: v.Status, Tsms: v.Tsms}, VapCheckConf{0, 0, 0, "", ""}, 0, 0, false, false, time.Now()}
			G_VapStatusCtl[v.Id] = vapctl
		}
	}

	return nil
}

func bootInit() error {

	agentLog.Init(logName)
	network.Init()
	if err := public.CpeConfigInit(); err != nil {
		agentLog.AgentLogger.Error("ConfigInit fail err: ", err)
		return err
	}

	return nil
}

func flagInit() error {
	logLevel := flag.Int("l", 2, "setLogLevel 0:debug 1:info 2:warning 3:error")
	flag.Parse()
	if err := agentLog.SetLevel(*logLevel); err != nil {
		return err
	}

	return nil
}

func main() {

	var err error

	if err := flagInit(); err != nil {
		agentLog.AgentLogger.Info("flagInit err:", err)
		return
	}
	err = bootInit()
	if err != nil {
		agentLog.AgentLogger.Info("bootInit err:", err)
		os.Exit(1)
	} else {
		agentLog.AgentLogger.Info("init monitor success.")
	}

	//save monitor status
	public.MakeDir(monitorpath)

	ips := make([]string, 1)
	ips[0] = "http://" + EtcdServer
	EtcdClient, err = etcd.NewEtcdClient(ips, "", "", "", false, "", "")
	if err != nil {
		agentLog.AgentLogger.Error("Etcd Client init failed.")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = getVapHistoryStatus(EtcdClient)
	if err != nil {
		agentLog.AgentLogger.Debug("get vap history status err:", err)
	}

	// 2s timer
	tick := time.NewTicker(time.Second * time.Duration(VapDetectInterval))
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			startStatusDetect()
		}
	}
}
