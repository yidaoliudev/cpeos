package main

import (
	"context"
	"cpeos/agentLog"
	"cpeos/app"
	"cpeos/config"
	"cpeos/etcd"
	"cpeos/public"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gitlab.daho.tech/gdaho/log"
)

var (
	logName           = "/var/log/cpe/statusreport.log"
	monitorpath       = "/var/run/monitor/"
	vnetRatePath      = "/var/log/vnet/rate.log"
	vnetStatusPath    = "/var/log/vnet/status.log"
	vnetLatencyPath   = "/var/log/vnet/latency.log"
	vnetLossPath      = "/var/log/vnet/loss.log"
	vnetPerfPath      = "/var/log/vnet/performance.log"
	DetectInterval    = 60
	Unknow            = 2
	Up                = 1
	Down              = 0
	HasChanged        = 1
	NoChanged         = 0
	NoPingLoss        = 200.00
	UnknowRate        = -200.00
	G_ReportStatusCtl = make(map[string]*ReportStatusCtl)
)

type ReportStatus struct {
	Id         string  `json:"id"`
	Type       string  `json:"type"`
	Rx_packets string  `json:"rxpackets"`
	Rx_bytes   string  `json:"rxbytes"`
	Tx_packets string  `json:"txpackets"`
	Tx_bytes   string  `json:"txbytes"`
	Rxrate     float64 `json:"rxrate"`
	Txrate     float64 `json:"txrate"`
	Latency    float64 `json:"latency"`
	Loss       float64 `json:"loss"`
	Status     int     `json:"status"`
	Changed    int     `json:"changed"`
	Cpuusage   float64 `json:"cpuusage"`
	Memusage   float64 `json:"memusage"`
	Diskusage  float64 `json:"diskusage"`
	Tsms       string  `json:"tsms"`
}

type ReportStatusCtl struct {
	ReportStatus
	AgeFlag bool
	ChgTime time.Time
}

type FlowStatusResult struct {
	Type        string
	Id          string
	RX_packets  string
	RX_bytes    string
	TX_packets  string
	TX_bytes    string
	TC_packets  string
	IPsec_estab string
	IPsec_insta string
	BGP_PfxRcd  int
	BGP_PfxSnt  int
	Delay       float64
	Loss        float64
	Status      int
	Changed     int
	StartTime   time.Time
}

type StatusBu struct {
	Sn             string `json:"sn"`
	Timestamp      uint64 `json:"timestamp"`
	BusinessName   string `json:"business_name"`
	BusinessID     string `json:"business_id"`
	BusinessStatus int8   `json:"business_status"`
	Rxrate         int64  `json:"rxrate"`
	Txrate         int64  `json:"txrate"`
	Sign           int8   `json:"sign"`
}

// 删除指定目录中所有具有特定后缀的文件
func deleteFilesWithSuffix(dir, suffix string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) == suffix {
			fmt.Printf("Deleting %s\n", path)
			return os.Remove(path)
		}
		return nil
	})
}

func WriteInfo(str string) {

	cpeinfo := "/var/run/cpeInfo"
	if public.FileExists(cpeinfo) {
		err := os.Remove(cpeinfo)
		if err != nil {
			agentLog.AgentLogger.Info(cpeinfo, "[ERROR]remove err: ", err)
			return
		}
	}

	fd, err := log.OpenLogFile(cpeinfo)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]open cpeinfo file err: ", err)
		return
	}
	defer fd.Close()

	n, err := fd.WriteString(str)
	if n != len(str) {
		agentLog.AgentLogger.Debug("write cpeinfo file err: ", err)

	}
	if err != nil {
		agentLog.AgentLogger.Debug(fmt.Sprintf("write cpeinfo file err,err: %v", err))
	}
}

func WriteInfoToInfluxdb(str, filepath string) {

	fd, err := log.OpenLogFile(filepath)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]open file err: ", filepath, err)
		return
	}
	defer fd.Close()

	n, err := fd.WriteString(str)
	if n != len(str) {
		agentLog.AgentLogger.Debug("write file err: ", filepath, err)

	}
	if err != nil {
		agentLog.AgentLogger.Debug(fmt.Sprintf("write file err: %v", err))
	}
}

func checkLossAll(pingresult string) (lossAllFlag bool) {
	expPackageLossall := regexp.MustCompile(`100% packet loss`)
	PackageLossall := expPackageLossall.FindAllString(pingresult, -1)

	if len(PackageLossall) != 0 {
		return true
	} else {
		return false
	}
}

func GetPingResult(pingresult string) (latency, packageloss, jitter float64) {
	if checkLossAll(pingresult) {
		return 0, 100, 0
	}

	expPacketLoss := regexp.MustCompile(`\d+% packet loss`)
	expcount := regexp.MustCompile(`\d+.\d+/\d+.\d+/\d+.\d+/\d+.\d+ ms`)
	result1 := expPacketLoss.FindAllString(pingresult, -1)
	result2 := expcount.FindAllString(pingresult, -1)
	packagelostlist := strings.Split(strings.Join(result1, ""), "%")
	avglist := strings.Split(strings.Join(result2, ""), "/")

	/* latency */
	rtt_avg := avglist[1]
	latency, _ = strconv.ParseFloat(rtt_avg, 64)

	/* loss */
	packageloststr := packagelostlist[0]
	packageloss, _ = strconv.ParseFloat(packageloststr, 64)

	/* jitter */
	rtt_mdev := strings.Split(avglist[3], " ") //avglist[3] = xxx ms
	jitter, _ = strconv.ParseFloat(rtt_mdev[0], 64)

	return latency, packageloss, jitter
}

func IfconfigStatusDetect(name string) (int, int) {

	var status = Down
	var changed = NoChanged
	filepath := monitorpath + name
	if public.FileExists(filepath) {
		status = Up
	}
	if public.FileExists(filepath + ".changed") {
		changed = HasChanged
	}
	return status, changed
}

func ConnStatusDetect(fp *app.ConnConf, chs chan FlowStatusResult) {

	var err error
	var loss float64 = 100
	var delay float64 = 0
	var context []string
	var result_str, rx_packets, rx_bytes, tx_packets, tx_bytes string

	/* 设置状态 */
	status, changed := IfconfigStatusDetect(fp.ConnName)

	/* health-check */
	checkAddress := ""
	if fp.Type == app.ConnType_Ipsec {
		if fp.IpsecInfo.HealthCheck {
			checkAddress = fp.IpsecInfo.RemoteAddress
		}
	} else if fp.Type == app.ConnType_Ssl {
		if fp.SslInfo.HealthCheck {
			checkAddress = fp.SslInfo.RemoteAddress
		}
	}

	if checkAddress != "" {
		pingcmd := fmt.Sprintf("ping -i 0.1 -c 10 %s -W 2", strings.Split(checkAddress, "/")[0])
		err, pingRet := public.ExecBashCmdWithRet(pingcmd)
		if err != nil {
			agentLog.AgentLogger.Info("ConnStatusDetect ping fail:", err, "pingcmd:", pingcmd, "pingRet:", pingRet, "fp", fp)
		}
		if pingRet != "" {
			delay, loss, _ = GetPingResult(pingRet)
		}
	} else {
		loss = NoPingLoss
	}

	/*接口流量统计*/
	cmdstr := fmt.Sprintf(`ifconfig %s | grep "packets" | awk '{print $3 "  " $5}' | awk BEGIN{RS=EOF}'{gsub(/\n/," ");print}'`, fp.ConnName)
	err, result_str = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("ConnStatusDetect:", cmdstr, err, "fp", fp)
	}
	context = strings.Fields(result_str)
	if len(context) != 4 {
		agentLog.AgentLogger.Info("bad context:", context, "fp", fp)
		rx_packets = "0"
		rx_bytes = "0"
		tx_packets = "0"
		tx_bytes = "0"

	} else {
		rx_packets = context[0]
		rx_bytes = context[1]
		tx_packets = context[2]
		tx_bytes = context[3]
	}

	chs <- FlowStatusResult{Type: "conn", Id: fp.Id, Status: status, Changed: changed, RX_packets: rx_packets, RX_bytes: rx_bytes, TX_packets: tx_packets, TX_bytes: tx_bytes, Delay: delay, Loss: loss}
}

func PortStatusDetect(fp *app.PortConf, chs chan FlowStatusResult) {

	var err error
	var loss float64 = 100
	var delay float64 = 0
	var context []string
	var result_str, rx_packets, rx_bytes, tx_packets, tx_bytes string

	/* Port 状态*/
	status, changed := IfconfigStatusDetect(fp.Id)

	/* health-check */
	checkAddress := ""
	if fp.Nexthop != "" {
		checkAddress = fp.Nexthop
	}

	if checkAddress != "" {
		pingcmd := fmt.Sprintf("ping -i 0.1 -c 10 %s -W 2", strings.Split(checkAddress, "/")[0])
		err, pingRet := public.ExecBashCmdWithRet(pingcmd)
		if err != nil {
			agentLog.AgentLogger.Info("PortStatusDetect ping fail:", err, "pingcmd:", pingcmd, "pingRet:", pingRet, "fp", fp)
		}
		if pingRet != "" {
			delay, loss, _ = GetPingResult(pingRet)
		}
	} else {
		loss = NoPingLoss
	}

	/*接口流量统计*/
	cmdstr := fmt.Sprintf(`ifconfig %s | grep "packets" | awk '{print $3 "  " $5}' | awk BEGIN{RS=EOF}'{gsub(/\n/," ");print}'`, fp.PhyifName)
	err, result_str = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("PortStatusDetect:", cmdstr, err, "fp", fp)
	}
	context = strings.Fields(result_str)
	if len(context) != 4 {
		agentLog.AgentLogger.Info("bad context:", context, "fp", fp)
		rx_packets = "0"
		rx_bytes = "0"
		tx_packets = "0"
		tx_bytes = "0"
	} else {
		rx_packets = context[0]
		rx_bytes = context[1]
		tx_packets = context[2]
		tx_bytes = context[3]
	}

	chs <- FlowStatusResult{Type: "port", Id: fp.Id, Status: status, Changed: changed, RX_packets: rx_packets, RX_bytes: rx_bytes, TX_packets: tx_packets, TX_bytes: tx_bytes, Delay: delay, Loss: loss}
}

////////////////////////////
////////////////////////////
////////////////////////////

func ConnStatusStrGet(conns map[string]string) (string, []StatusBu) {
	var kfkInfos []StatusBu

	chs := make(chan FlowStatusResult)
	res_num := len(conns)
	if res_num == 0 {
		return "", kfkInfos
	}
	resultList := make([]FlowStatusResult, res_num)

	for _, value := range conns {
		bytes := []byte(value)
		fp := &app.ConnConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]conn data unmarshal failed: ", err.Error(), "fp", fp)
			continue
		}
		go ConnStatusDetect(fp, chs)
	}

	for i := 0; i < res_num; i++ {
		res := <-chs
		resultList[i] = res
	}

	var builder strings.Builder

	for _, res := range resultList {
		builder.WriteString(fmt.Sprintf("cpe_conn_status{sn=\"%s\",  id=\"%s\"} %d\n", public.G_coreConf.Sn, res.Id, res.Status))
		builder.WriteString(fmt.Sprintf("cpe_conn_rxrate{sn=\"%s\",  id=\"%s\"} %s\n", public.G_coreConf.Sn, res.Id, res.RX_bytes))
		builder.WriteString(fmt.Sprintf("cpe_conn_txrate{sn=\"%s\",  id=\"%s\"} %s\n", public.G_coreConf.Sn, res.Id, res.TX_bytes))
		if res.Loss != NoPingLoss {
			builder.WriteString(fmt.Sprintf("cpe_conn_delay{sn=\"%s\", id=\"%s\"} %.2f\n", public.G_coreConf.Sn, res.Id, res.Delay))
			builder.WriteString(fmt.Sprintf("cpe_conn_loss{sn=\"%s\",  id=\"%s\"} %.2f\n", public.G_coreConf.Sn, res.Id, res.Loss))
		}
		tmp := KfkInfoCreate("conn", res)
		kfkInfos = append(kfkInfos, tmp)

		//influxdb
		_, ok := G_ReportStatusCtl[res.Id]
		if !ok {
			G_ReportStatusCtl[res.Id] = &ReportStatusCtl{ReportStatus{Id: res.Id, Type: "conn", Rx_bytes: res.RX_bytes, Rx_packets: res.RX_packets, Tx_bytes: res.TX_bytes, Tx_packets: res.TX_packets, Status: Unknow}, false, time.Now()}
			G_ReportStatusCtl[res.Id].Rxrate = UnknowRate
			G_ReportStatusCtl[res.Id].Txrate = UnknowRate
		} else {
			//如果已经存在，则计算Rate
			new, _ := strconv.ParseInt(res.RX_bytes, 10, 64)
			old, _ := strconv.ParseInt(G_ReportStatusCtl[res.Id].Rx_bytes, 10, 64)
			G_ReportStatusCtl[res.Id].Rxrate = float64(new-old) / 60 * 8
			new, _ = strconv.ParseInt(res.TX_bytes, 10, 64)
			old, _ = strconv.ParseInt(G_ReportStatusCtl[res.Id].Tx_bytes, 10, 64)
			G_ReportStatusCtl[res.Id].Txrate = float64(new-old) / 60 * 8

			G_ReportStatusCtl[res.Id].Rx_bytes = res.RX_bytes
			G_ReportStatusCtl[res.Id].Tx_bytes = res.TX_bytes
			G_ReportStatusCtl[res.Id].Rx_packets = res.RX_packets
			G_ReportStatusCtl[res.Id].Tx_packets = res.TX_packets
		}
		G_ReportStatusCtl[res.Id].Loss = res.Loss
		G_ReportStatusCtl[res.Id].Latency = res.Delay
		G_ReportStatusCtl[res.Id].Status = res.Status
		G_ReportStatusCtl[res.Id].Changed = res.Changed
		G_ReportStatusCtl[res.Id].AgeFlag = false
	}

	str := builder.String()
	return str, kfkInfos
}

func PortStatusStrGet(ports map[string]string) (string, []StatusBu) {

	var kfkInfos []StatusBu

	chs := make(chan FlowStatusResult)
	res_num := len(ports)
	if res_num == 0 {
		return "", kfkInfos
	}
	resultList := make([]FlowStatusResult, res_num)

	for _, value := range ports {
		bytes := []byte(value)
		fp := &app.PortConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]port data unmarshal failed: ", err.Error(), "fp", fp)
			continue
		}
		go PortStatusDetect(fp, chs)
	}

	for i := 0; i < res_num; i++ {
		res := <-chs
		resultList[i] = res
	}

	var builder strings.Builder

	for _, res := range resultList {
		builder.WriteString(fmt.Sprintf("cpe_port_status{sn=\"%s\", id=\"%s\"} %d\n", public.G_coreConf.Sn, res.Id, res.Status))
		builder.WriteString(fmt.Sprintf("cpe_port_rxrate{sn=\"%s\", id=\"%s\"} %s\n", public.G_coreConf.Sn, res.Id, res.RX_bytes))
		builder.WriteString(fmt.Sprintf("cpe_port_txrate{sn=\"%s\", id=\"%s\"} %s\n", public.G_coreConf.Sn, res.Id, res.TX_bytes))
		if res.Loss != NoPingLoss {
			builder.WriteString(fmt.Sprintf("cpe_port_delay{sn=\"%s\", id=\"%s\"} %.2f\n", public.G_coreConf.Sn, res.Id, res.Delay))
			builder.WriteString(fmt.Sprintf("cpe_port_loss{sn=\"%s\", id=\"%s\"} %.2f\n", public.G_coreConf.Sn, res.Id, res.Loss))
		}
		tmp := KfkInfoCreate("port", res)
		kfkInfos = append(kfkInfos, tmp)

		//influxdb
		_, ok := G_ReportStatusCtl[res.Id]
		if !ok {
			G_ReportStatusCtl[res.Id] = &ReportStatusCtl{ReportStatus{Id: res.Id, Type: "port", Rx_bytes: res.RX_bytes, Rx_packets: res.RX_packets, Tx_bytes: res.TX_bytes, Tx_packets: res.TX_packets, Status: Unknow}, false, time.Now()}
			G_ReportStatusCtl[res.Id].Rxrate = UnknowRate
			G_ReportStatusCtl[res.Id].Txrate = UnknowRate
		} else {
			//如果已经存在，则计算Rate
			new, _ := strconv.ParseInt(res.RX_bytes, 10, 64)
			old, _ := strconv.ParseInt(G_ReportStatusCtl[res.Id].Rx_bytes, 10, 64)
			G_ReportStatusCtl[res.Id].Rxrate = float64(new-old) / 60 * 8
			new, _ = strconv.ParseInt(res.TX_bytes, 10, 64)
			old, _ = strconv.ParseInt(G_ReportStatusCtl[res.Id].Tx_bytes, 10, 64)
			G_ReportStatusCtl[res.Id].Txrate = float64(new-old) / 60 * 8

			G_ReportStatusCtl[res.Id].Rx_bytes = res.RX_bytes
			G_ReportStatusCtl[res.Id].Tx_bytes = res.TX_bytes
			G_ReportStatusCtl[res.Id].Rx_packets = res.RX_packets
			G_ReportStatusCtl[res.Id].Tx_packets = res.TX_packets
		}
		G_ReportStatusCtl[res.Id].Loss = res.Loss
		G_ReportStatusCtl[res.Id].Latency = res.Delay
		G_ReportStatusCtl[res.Id].Status = res.Status
		G_ReportStatusCtl[res.Id].Changed = res.Changed
		G_ReportStatusCtl[res.Id].AgeFlag = false
	}

	str := builder.String()
	return str, kfkInfos
}

func SysInfoStrGet() string {
	var context []string
	var diskAvail, diskUsage string
	var builder strings.Builder

	/* cpu usage */
	cmdstr := "mpstat -P ALL 1 1  | grep -v 'CPU' | grep  'Average' | awk '{print $12}'"
	//cmdstr := "sar -u 1 1 | tail -n 1| awk '{print $8}'"
	err, cpuusage_str := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("cmdstr:", cmdstr, err, "result_str", cpuusage_str)
		return ""
	}
	cpu := 0.00
	cpuusage_float := 0.00
	context = strings.Fields(cpuusage_str)
	if len(context) >= 1 {
		cpuusage_float, _ = strconv.ParseFloat(strings.Replace(context[0], "\n", "", -1), 64)
		cpu = 100.00 - cpuusage_float
		builder.WriteString(fmt.Sprintf("cpe_sys_cpuusage{sn=\"%s\"} %.2f\n", public.G_coreConf.Sn, (100.00 - cpuusage_float)))
	}

	if len(context) >= 2 {
		cpuusage_float, _ = strconv.ParseFloat(strings.Replace(context[1], "\n", "", -1), 64)
		builder.WriteString(fmt.Sprintf("cpe_sys_cpuoneusage{sn=\"%s\", id=\"%s\"} %.2f\n", public.G_coreConf.Sn, "cpu0", (100.00 - cpuusage_float)))
	}
	if len(context) >= 3 {
		cpuusage_float, _ = strconv.ParseFloat(strings.Replace(context[2], "\n", "", -1), 64)
		builder.WriteString(fmt.Sprintf("cpe_sys_cpuoneusage{sn=\"%s\", id=\"%s\"} %.2f\n", public.G_coreConf.Sn, "cpu1", (100.00 - cpuusage_float)))
	}
	if len(context) >= 5 {
		cpuusage_float, _ = strconv.ParseFloat(strings.Replace(context[3], "\n", "", -1), 64)
		builder.WriteString(fmt.Sprintf("cpe_sys_cpuoneusage{sn=\"%s\", id=\"%s\"} %.2f\n", public.G_coreConf.Sn, "cpu2", (100.00 - cpuusage_float)))
		cpuusage_float, _ = strconv.ParseFloat(strings.Replace(context[4], "\n", "", -1), 64)
		builder.WriteString(fmt.Sprintf("cpe_sys_cpuoneusage{sn=\"%s\", id=\"%s\"} %.2f\n", public.G_coreConf.Sn, "cpu3", (100.00 - cpuusage_float)))
	}

	/* mem usage */
	cmdstr = "free -k | sed -n '2p' | awk '{print $2 \" \" $7}'"
	err, memusage_str := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("cmdstr:", cmdstr, err, "result_str", memusage_str)
		return ""
	}
	context = strings.Fields(memusage_str)
	memavail_float, _ := strconv.ParseFloat(context[1], 64)
	memTotal_float, _ := strconv.ParseFloat(context[0], 64)
	mem := 100.00 - (memavail_float * 100 / memTotal_float)
	builder.WriteString(fmt.Sprintf("cpe_sys_memavailable{sn=\"%s\"} %s\n", public.G_coreConf.Sn, context[1]))
	builder.WriteString(fmt.Sprintf("cpe_sys_memusage{sn=\"%s\"} %.2f\n", public.G_coreConf.Sn, 100.00-(memavail_float*100/memTotal_float)))

	/* disk usage */
	cmdstr = "df -Phk | grep '^/dev/*' | head -n 1 | awk '{print $4 \" \" $5}'"
	err, diskusage_str := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("cmdstr:", cmdstr, err, "result_str", diskusage_str)
		return ""
	}
	context = strings.Fields(diskusage_str)
	diskAvail = context[0]
	diskUsage = strings.Split(context[1], "%")[0]
	disk, _ := strconv.ParseFloat(strings.Replace(diskUsage, "\n", "", -1), 64)
	builder.WriteString(fmt.Sprintf("cpe_sys_diskavailable{sn=\"%s\"} %s\n", public.G_coreConf.Sn, diskAvail))
	builder.WriteString(fmt.Sprintf("cpe_sys_diskusage{sn=\"%s\"} %s\n", public.G_coreConf.Sn, diskUsage))
	str := builder.String()

	//influxdb
	id := "performance"
	_, ok := G_ReportStatusCtl[id]
	if !ok {
		G_ReportStatusCtl[id] = &ReportStatusCtl{ReportStatus{Id: id, Type: "system", Cpuusage: cpu, Memusage: mem, Diskusage: disk, Status: Up}, false, time.Now()}
	} else {
		G_ReportStatusCtl[id].Cpuusage = cpu
		G_ReportStatusCtl[id].Memusage = mem
		G_ReportStatusCtl[id].Diskusage = disk
	}
	G_ReportStatusCtl[id].AgeFlag = false

	return str
}

func LogLineNumStrGet() string {

	cmdstr := fmt.Sprintf(`wc -l %s | awk '{print $1 }'`, "/var/log/cpe/cpe.log")
	err, result_str := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("cmdstr:", cmdstr, err, "result_str", result_str)
		return ""
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("cpe_log_linenum{sn=\"%s\"} %s", public.G_coreConf.Sn, result_str))
	str := builder.String()
	return str
}

func LogMaxFileStrGet() string {

	cmdstr := fmt.Sprintf(`find /var/log/ -type f -printf '%%s %%p\n' | sort -nr | head -1 | awk  '{print $1}'`)
	err, result_str := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("cmdstr:", cmdstr, err, "result_str", result_str)
		return ""
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("cpe_log_maxfile{sn=\"%s\"} %s", public.G_coreConf.Sn, result_str))
	str := builder.String()
	return str
}

func KernelParaStrGet() string {

	ipforward := "1"
	cmdstr := "cat /proc/sys/net/ipv4/ip_forward"
	err, result_str := public.ExecBashCmdWithRet(cmdstr)
	if err == nil {
		ipforward = strings.Replace(result_str, "\n", "", -1)
	}

	if ipforward != "1" {
		cmdstr = "sysctl -w net.ipv4.ip_forward=1"
		err, result_str = public.ExecBashCmdWithRet(cmdstr)
		if err == nil {
			ipforward = "1"
		}
	}

	rpfilter := "0"
	cmdstr = "cat /proc/sys/net/ipv4/conf/all/rp_filter"
	err, result_str = public.ExecBashCmdWithRet(cmdstr)
	if err == nil {
		rpfilter = strings.Replace(result_str, "\n", "", -1)
	}

	if rpfilter != "0" {
		cmdstr = "sysctl -w net.ipv4.conf.all.rp_filter=0"
		err, result_str = public.ExecBashCmdWithRet(cmdstr)
		if err == nil {
			rpfilter = "0"
		}
	}

	conntrackCount := "0"
	cmdstr = "cat /proc/sys/net/netfilter/nf_conntrack_count"
	err, result_str = public.ExecBashCmdWithRet(cmdstr)
	if err == nil {
		conntrackCount = strings.Replace(result_str, "\n", "", -1)
	}

	conntrackMax := "1000000"
	cmdstr = "cat /proc/sys/net/nf_conntrack_max"
	err, result_str = public.ExecBashCmdWithRet(cmdstr)
	if err == nil {
		conntrackMax = strings.Replace(result_str, "\n", "", -1)
	}

	/* 比较当前占比，如果在100万以内，则自动调整 */
	floatConnCount, _ := strconv.ParseFloat(conntrackCount, 64)
	floatConnMax, _ := strconv.ParseFloat(conntrackMax, 64)
	if floatConnMax < 1000000 && floatConnCount > floatConnMax*0.8 {
		cmdstr = "sysctl -w net.netfilter.nf_conntrack_max=1000000"
		err, result_str = public.ExecBashCmdWithRet(cmdstr)
		if err == nil {
			conntrackMax = "1000000"
		}
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("cpe_kpara_ipforward{sn=\"%s\"} %s\n", public.G_coreConf.Sn, ipforward))
	builder.WriteString(fmt.Sprintf("cpe_kpara_rpfilter{sn=\"%s\"} %s\n", public.G_coreConf.Sn, rpfilter))
	builder.WriteString(fmt.Sprintf("cpe_kpara_conncount{sn=\"%s\"} %s\n", public.G_coreConf.Sn, conntrackCount))
	builder.WriteString(fmt.Sprintf("cpe_kpara_connmax{sn=\"%s\"} %s\n", public.G_coreConf.Sn, conntrackMax))
	str := builder.String()
	return str
}

func SiteRoleStrGet() string {
	site, err := etcd.EtcdGetValue(config.SiteConfPath)
	if err != nil {
		agentLog.AgentLogger.Info("site not found: ", err.Error())
		return ""
	}

	bytes := []byte(site)
	fp := &app.SiteConf{}
	err = json.Unmarshal(bytes, fp)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]site data unmarshal failed: ", err.Error())
		return ""
	}

	if fp.Id == "" {
		return ""
	}

	ha, err := etcd.EtcdGetValue(config.HaConfPath)
	if err != nil {
		agentLog.AgentLogger.Info("ha not found: ", err.Error())
		return ""
	}
	bytes2 := []byte(ha)
	fp2 := &app.HaConf{}
	err = json.Unmarshal(bytes2, fp2)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]ha data unmarshal failed: ", err.Error())
		return ""
	}

	if fp2.Enable && fp2.Role == app.HaRole_Backup {
		return fp.Id + "-slave"
	}

	return fp.Id + "-master"
}

func UpdateStatus2InfluxDb(now time.Time, siteRole string) error {

	var rate_builder, status_builder, latency_builder, loss_builder, perf_builder strings.Builder
	//timestamp := time.Now().UnixNano()
	//now := time.Now()
	// 创建当天的开始时间（00:00:00）并加上当前分钟数的时间
	startOfMinute := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), 0, 0, now.Location())
	// 获取整分钟时间戳
	timestamp := startOfMinute.UnixNano()

	for _, v := range G_ReportStatusCtl {
		if v.Status == Unknow {
			continue
		}

		if v.Type == "conn" {
			if siteRole != "" {
				rate_builder.WriteString(fmt.Sprintf("rate,uid=%s,type=%s,dev=%s rate_rx=%.2f,rate_tx=%.2f %d\n", v.Id, "connection", siteRole, v.Rxrate, v.Txrate, timestamp))
				status_builder.WriteString(fmt.Sprintf("status,uid=%s,type=%s,dev=%s status=%d,changed=%d %d\n", v.Id, "connection", siteRole, v.Status, v.Changed, timestamp))
				if v.Loss != NoPingLoss {
					latency_builder.WriteString(fmt.Sprintf("latency,uid=%s,type=%s,dev=%s latency=%.2f %d\n", v.Id, "connection", siteRole, v.Latency, timestamp))
					loss_builder.WriteString(fmt.Sprintf("loss,uid=%s,type=%s,dev=%s loss=%.0f %d\n", v.Id, "connection", siteRole, v.Loss, timestamp))
				}
			}
		} else if v.Type == "port" {
			if siteRole != "" {
				rate_builder.WriteString(fmt.Sprintf("rate,uid=%s,type=%s,dev=%s rate_rx=%.2f,rate_tx=%.2f %d\n", v.Id, "port", siteRole, v.Rxrate, v.Txrate, timestamp))
				status_builder.WriteString(fmt.Sprintf("status,uid=%s,type=%s,dev=%s status=%d,changed=%d %d\n", v.Id, "port", siteRole, v.Status, v.Changed, timestamp))
				if v.Loss != NoPingLoss {
					latency_builder.WriteString(fmt.Sprintf("latency,uid=%s,type=%s,dev=%s latency=%.2f %d\n", v.Id, "port", siteRole, v.Latency, timestamp))
					loss_builder.WriteString(fmt.Sprintf("loss,uid=%s,type=%s,dev=%s loss=%.0f %d\n", v.Id, "port", siteRole, v.Loss, timestamp))
				}
			}
		} else if v.Type == "system" {
			perf_builder.WriteString(fmt.Sprintf("%s,dev=%s cpu=%.2f,memory=%.2f,disk=%.2f %d\n", v.Id, public.G_coreConf.Sn, v.Cpuusage, v.Memusage, v.Diskusage, timestamp))
		}
	}

	str := rate_builder.String()
	if true || str != "" {
		WriteInfoToInfluxdb(str, vnetRatePath)
	}
	str = status_builder.String()
	if true || str != "" {
		WriteInfoToInfluxdb(str, vnetStatusPath)
	}
	str = latency_builder.String()
	if true || str != "" {
		WriteInfoToInfluxdb(str, vnetLatencyPath)
	}
	str = loss_builder.String()
	if true || str != "" {
		WriteInfoToInfluxdb(str, vnetLossPath)
	}
	str = perf_builder.String()
	if true || str != "" {
		WriteInfoToInfluxdb(str, vnetPerfPath)
	}

	deleteFilesWithSuffix(monitorpath, ".changed")
	return nil
}

func ageReportStart() {
	for _, v := range G_ReportStatusCtl {
		v.AgeFlag = true
	}
}

func ageReportEnd() bool {
	aged := false
	//log.Info("age end.")
	for id, v := range G_ReportStatusCtl {
		if v.AgeFlag {
			delete(G_ReportStatusCtl, id)
			log.Info("delete report %v.", id)
			aged = true
		}
	}
	return aged
}

func startDetect() error {

	now := time.Now()
	var builder strings.Builder
	//var kfkInfos []StatusBu

	paths := []string{config.PortConfPath}
	ports, _ := etcd.EtcdGetValues(paths)

	paths = []string{config.ConnConfPath}
	conns, _ := etcd.EtcdGetValues(paths)

	// 老化开始，置老化标记
	ageReportStart()
	portStatusStr, _ := PortStatusStrGet(ports)
	if portStatusStr != "" {
		builder.WriteString(portStatusStr)
		//kfkInfos = append(kfkInfos, tmp...)
	}

	connStatusStr, _ := ConnStatusStrGet(conns)
	if connStatusStr != "" {
		builder.WriteString(connStatusStr)
		//kfkInfos = append(kfkInfos, tmp...)
	}

	sysInfoStr := SysInfoStrGet()
	if sysInfoStr != "" {
		builder.WriteString(sysInfoStr)
	}

	// 老化开始，置老化标记
	ageReportEnd()

	siteRole := SiteRoleStrGet()
	// 处理report
	err := UpdateStatus2InfluxDb(now, siteRole)
	if err != nil {
		agentLog.AgentLogger.Debug("set report status to influxdb err: %v", err)
	}

	logLineNumStr := LogLineNumStrGet()
	if logLineNumStr != "" {
		builder.WriteString(logLineNumStr)
	}

	logMaxFileStr := LogMaxFileStrGet()
	if logMaxFileStr != "" {
		builder.WriteString(logMaxFileStr)
	}

	kernelParaStr := KernelParaStrGet()
	if kernelParaStr != "" {
		builder.WriteString(kernelParaStr)
	}

	str := builder.String()
	if true || str != "" {
		WriteInfo(str)
	}

	return nil
}

func KfkInfoCreate(businessName string, res FlowStatusResult) StatusBu {
	var kfkInfo StatusBu
	kfkInfo.Sn = public.G_coreConf.Sn
	//kfkInfo.Timestamp = uint64(time.Now().UnixMilli())
	kfkInfo.BusinessName = businessName
	kfkInfo.BusinessID = res.Id
	kfkInfo.BusinessStatus = int8(res.Status)
	//kfkInfo.Rxrate, _ = strconv.ParseInt(res.RX_bytes, 10, 64)
	//kfkInfo.Txrate, _ = strconv.ParseInt(res.TX_bytes, 10, 64)
	kfkInfo.Sign = 1

	return kfkInfo
}

func CpeTask(ctx context.Context) error {

	// 获取下一个整分钟的开始时间
	now := time.Now()
	nextMinute := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), 0, 0, now.Location())
	nextMinute = nextMinute.Add(time.Minute)

	// 计算时间间隔
	d := nextMinute.Sub(now)

	// 使用time.NewTimer创建定时器
	timer := time.NewTimer(d)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			// 重置定时器
			timer.Reset(time.Minute)
			startDetect()
		}
	}
}

func main() {
	agentLog.Init(logName)
	err := etcd.Etcdinit()
	if err != nil {
		agentLog.AgentLogger.Error("init etcd failed: ", err.Error())
		return
	}

	//save monitor status
	public.MakeDir("/var/log/vnet/")

	if err = public.CpeConfigInit(); err != nil {
		agentLog.AgentLogger.Error("CpeConfigInit fail err: ", err)
		return
	}

	agentLog.AgentLogger.Info("init statusreport success.")
	ctx, _ := context.WithCancel(context.Background())
	CpeTask(ctx)
}
