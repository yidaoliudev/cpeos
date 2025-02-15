package main

import (
	"context"
	"cpeos/agentLog"
	"cpeos/etcd"
	"cpeos/public"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"gitlab.daho.tech/gdaho/log"
)

type AddrCheckConf struct {
	Priority int
	Target   string
}

type AddrStatus struct {
	Id     string `json:"id"`
	Tsms   string `json:"tsms"`
	Status string `json:"status"`
}

type AddrStatusCtl struct {
	AddrStatus
	Conf    AddrCheckConf
	DownCnt int
	AgeFlag bool
	ChgTime time.Time
}

type AddrDelayResult struct {
	Addr      string
	Delay     float64
	StartTime time.Time
}

type CoreConf struct {
	Success bool   `json:"success"`
	Ret     int    `json:"ret"`
	Code    string `json:"code"`
	Msg     string `json:"msg"`
	Data    string `json:"data"`
}

var (
	logName          = "/var/log/cpe/discover.log"
	G_AddrStatusCtl  = make(map[string]*AddrStatusCtl)
	G_SelectCoreAddr = ""
	G_SelectHoldTime = 0
)

const (
	coreListPath   = "/mnt/agent/coreList.conf"
	sysHostsPath   = "/etc/hosts"
	fakeLatency    = 0xFFFF
	DOWNCNT_MAX    = 3
	DetectInterval = 30
	DetectHoldCnt  = 240 /* default 240，约2小时*/
)

func getCoreAddrFormHosts() string {

	/* 获取当前CoreAddress对应在/etc/hosts里的ip信息 */
	ipAddr := ""
	cmdstr := fmt.Sprintf("cat %s | grep %s | awk '{print $1}'", sysHostsPath, public.G_coreConf.CoreAddress)
	err, result_str := public.ExecBashCmdWithRet(cmdstr)
	if err == nil {
		ipAddr = strings.Replace(result_str, "\n", "", -1)
	}

	return ipAddr
}

func setCoreAddressToHosts(addr string) error {

	/* 先把域名对于的解析行配置删除 */
	cmdstr := fmt.Sprintf("sed -i '/%s/d' %s", public.G_coreConf.CoreAddress, sysHostsPath)
	err, _ := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("Discover setCoreAddressToHosts remove hosts error, Address: ", addr)
	}

	/* 再增加一列新的域名解析到文件末尾 */
	cmdstr = fmt.Sprintf("echo '%s %s' >> %s", addr, public.G_coreConf.CoreAddress, sysHostsPath)
	err, _ = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("Discover setCoreAddressToHosts setAddr to hosts error, Address: ", addr)
	}

	agentLog.AgentLogger.Info("Discover update hosts ", public.G_coreConf.CoreAddress, " to address : ", addr)
	return nil
}

func getCoreAddressListMap() (map[string]*AddrStatusCtl, error) {

	coreAddrMap := make(map[string]*AddrStatusCtl)
	timeStr := fmt.Sprintf("%d", time.Now().UnixNano())

	/* 如果文件不存在，则不检测 */
	if !public.FileExists(coreListPath) {
		return coreAddrMap, nil
	}

	data, err := ioutil.ReadFile(coreListPath) //read the file
	if err != nil {
		agentLog.AgentLogger.Info("Discover readFile error: ", coreListPath)
		return coreAddrMap, nil
	}

	prio := 1
	ipList := strings.Split(string(data), "\n")
	for _, member := range ipList {
		if member == "" {
			// 如果内容是空
			continue
		}
		coreAddrMap[member] = &AddrStatusCtl{AddrStatus{Id: member, Status: public.VAPUNKNOWN, Tsms: timeStr},
			AddrCheckConf{prio, member}, 0, false, time.Now()}
		prio++
	}

	return coreAddrMap, nil
}

func discoverAddrStatus(addr *AddrStatusCtl, chs chan AddrDelayResult) {

	/* 通过调用Core的API */
	url := "/api/cpeConfig/ops/echo"
	respBody, err := public.GetRequestCore(addr.Conf.Target, public.G_coreConf.CorePort, public.G_coreConf.CoreProto, url)
	if err == nil {
		/* 转化成json */
		conf := &CoreConf{}
		if err := json.Unmarshal(respBody, conf); err == nil {
			if conf.Success && conf.Ret == 0 {
				// 写入一个非0时延，模拟up状态
				chs <- AddrDelayResult{Addr: addr.Id, Delay: fakeLatency, StartTime: time.Now()}
				return
			}
		}
	}

	// 写入一个0时延，模拟down状态
	chs <- AddrDelayResult{Addr: addr.Id, Delay: 0, StartTime: time.Now()}
}

func ageAddrListStart() {
	for _, v := range G_AddrStatusCtl {
		v.AgeFlag = true
	}
}

func ageAddrListEnd() bool {
	aged := false
	//log.Info("age end.")
	for addr, vapS := range G_AddrStatusCtl {
		if vapS.AgeFlag {
			delete(G_AddrStatusCtl, addr)
			log.Info("delete addr %v.", addr)
			aged = true
		}
	}
	return aged
}

func updateAddrsStatus(resultList []AddrDelayResult) error {

	bestAddr := ""
	priority := 100
	for _, ins := range resultList {
		if _, ok := G_AddrStatusCtl[ins.Addr]; ok {
			addrStatus := G_AddrStatusCtl[ins.Addr]
			addrStatus.DownCnt = 0
			if ins.Delay != 0 {
				addrStatus.Status = public.VAPNORMAL
				if addrStatus.Conf.Priority < priority {
					priority = addrStatus.Conf.Priority
					bestAddr = addrStatus.Id
				}
			}
		}
	}

	if bestAddr != "" {
		/* 获取当前配置的地址信息 */
		ipAddr := getCoreAddrFormHosts()
		if ipAddr != bestAddr {
			err := setCoreAddressToHosts(bestAddr)
			if err == nil {
				G_SelectCoreAddr = bestAddr
			}
		} else {
			G_SelectCoreAddr = bestAddr
		}
	}

	return nil
}

func checkAddrStatus(result AddrDelayResult) error {

	if _, ok := G_AddrStatusCtl[result.Addr]; ok {
		addrStatus := G_AddrStatusCtl[result.Addr]
		if result.Delay == 0 {
			addrStatus.DownCnt++
			if result.Delay == 0 && addrStatus.Status != public.VAPOFFLINE && addrStatus.DownCnt >= DOWNCNT_MAX {
				addrStatus.Status = public.VAPOFFLINE
				addrStatus.ChgTime = result.StartTime
				agentLog.AgentLogger.Info("Discover [", addrStatus.Id, "] Status Change to Down.")
				G_SelectCoreAddr = ""
			}
		}
	}

	return nil
}

func startDiscover() error {

	/* 获取coreList最新配置 */
	addrListMap, _ := getCoreAddressListMap()
	num := len(addrListMap)
	if num == 0 {
		return nil
	}

	// 老化开始，置老化标记
	ageAddrListStart()
	for _, addr := range addrListMap {
		_, ok := G_AddrStatusCtl[addr.Id]
		if ok {
			G_AddrStatusCtl[addr.Id].AgeFlag = false
		}
	}
	// 老化结束，没有更新标记的全部老化
	ageChg := ageAddrListEnd()

	// 检查地址配置是否有变化
	cfgChg := false
	for _, addr := range addrListMap {
		_, ok := G_AddrStatusCtl[addr.Id]
		if !ok {
			cfgChg = true
		} else {
			//如果已经存在，比较优先级
			if G_AddrStatusCtl[addr.Id].Conf.Priority != addr.Conf.Priority {
				cfgChg = true
			}
		}
	}

	// 如果有地址老化/新增/优先级变化 或者 持续次数超限，则重新select.
	if ageChg || cfgChg || G_SelectHoldTime > DetectHoldCnt {
		/* 如果配置有变更，则重新selecting */
		/* 如果坚持次数达到上限，则重新Selecting */
		G_SelectCoreAddr = ""
	}

	if G_SelectCoreAddr == "" {
		// selecting
		G_SelectHoldTime = 0
		chs := make(chan AddrDelayResult)
		resultList := make([]AddrDelayResult, num)
		for _, addr := range addrListMap {
			_, ok := G_AddrStatusCtl[addr.Id]
			if !ok {
				G_AddrStatusCtl[addr.Id] = addr
			} else {
				//如果已经存在，则同步配置
				G_AddrStatusCtl[addr.Id].Conf = addr.Conf
			}
			go discoverAddrStatus(addr, chs)
		}

		// 抓取返回结果
		for i := 0; i < num; i++ {
			res := <-chs
			resultList[i] = res
		}

		// 结果处理
		updateAddrsStatus(resultList)
	} else {
		//offering, 探测生效coreAddr
		G_SelectHoldTime++
		chs := make(chan AddrDelayResult)
		addr, ok := G_AddrStatusCtl[G_SelectCoreAddr]
		if ok {
			go discoverAddrStatus(addr, chs)
			res := <-chs
			result := res

			// 结果处理
			checkAddrStatus(result)
		} else {
			G_SelectCoreAddr = ""
		}
	}

	return nil
}

func main() {
	agentLog.Init(logName)
	err := etcd.Etcdinit()
	if err != nil {
		agentLog.AgentLogger.Error("init etcd failed: ", err.Error())
		return
	}

	if err = public.CpeConfigInit(); err != nil {
		agentLog.AgentLogger.Error("CpeConfigInit fail err: ", err)
		return
	}

	agentLog.AgentLogger.Info("init discover success.")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 30秒 timer
	tick := time.NewTicker(time.Second * time.Duration(DetectInterval))
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			startDiscover()
		}
	}
}
