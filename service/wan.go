package main

import (
	"context"
	"cpeos/agentLog"
	"cpeos/app"
	"cpeos/public"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

const (
	wanMonitorInterval = 10
	haLocalNatPath     = "/var/run/haLocalNat"
)

type WanStatus struct {
	id      string
	ipAddr  string
	nexthop string
	ipType  string
	phyif   string
	chgFlag bool /* 上报core的flag */
	synFlag bool /* 同步etcd的flag*/
}

type reportWanInfo struct {
	Name     string `json:"name"` /* wan1, wan2 ... */
	IpAddr   string `json:"ipAddr"`
	Gateway  string `json:"gateway"`
	IpSource string `json:"ipSource"` /* static or dhcp */
}

type reportHaInfo struct {
	Status string `json:"status"` /* ACTIVE, INACTIVE */
}

/*
*
概述：Wan接口地址信息监控。
*
*/
func wanStatusUpdate(id string, wanStatus map[string]*WanStatus, ipAddr, nexthop, ipType, phyif string) error {

	curWan := wanStatus[id]
	if curWan != nil {
		if curWan.ipAddr != ipAddr || curWan.nexthop != nexthop || curWan.ipType != ipType {
			curWan.ipAddr = ipAddr
			curWan.nexthop = nexthop
			curWan.ipType = ipType
			curWan.chgFlag = true
			curWan.synFlag = true
		}
	} else {
		wanStatus[id] = &WanStatus{id, ipAddr, nexthop, ipType, phyif, true, true}
		wanStatus[id].chgFlag = true
		wanStatus[id].synFlag = true
	}

	return nil
}

func wanStatusReportCore(wanStatus map[string]*WanStatus) error {

	var reportInfo reportWanInfo

	for _, status := range wanStatus {

		/* 更新etcd的port实例信息，以及对应的路由表 */
		if status.synFlag {
			port := app.PortConf{Id: status.id, IpAddr: status.ipAddr, Nexthop: status.nexthop, PhyifName: status.phyif}
			err, chg, curNexthop, curIpAddr := UpdateWan(&port)
			if err == nil {
				status.synFlag = false
			}

			if chg && curNexthop != status.nexthop {
				curPort := app.PortConf{Id: status.id, IpAddr: curIpAddr, Nexthop: curNexthop, PhyifName: status.phyif}
				app.UpdateNexthop(&curPort, status.nexthop, status.ipAddr)
			}
		}

		if status.chgFlag {
			reportInfo.Name = status.id
			reportInfo.IpAddr = status.ipAddr
			reportInfo.Gateway = status.nexthop
			reportInfo.IpSource = status.ipType
			bytedata, err := json.Marshal(reportInfo)
			if err != nil {
				agentLog.AgentLogger.Info("[ERROR]Post core logicPorts/wan err:", err)
				return err
			}

			url := fmt.Sprintf("/api/cpeConfig/cpes/%s/logicPorts/wan", public.G_coreConf.Sn)
			_, err = public.RequestCore(bytedata, public.G_coreConf.CoreAddress, public.G_coreConf.CorePort, public.G_coreConf.CoreProto, url)
			if err != nil {
				agentLog.AgentLogger.Info("[ERROR]Post core logicPorts/wan err:", err, string(bytedata[:]), ", url:", url)
				///log.Warning("Post core logicPorts/wan err:", err, bytedata, ", url:", url)
			} else {
				status.chgFlag = false
				agentLog.AgentLogger.Info("Post core logicPorts/wan success, Id: ", status.id, ", reportInfo: ", string(bytedata[:]))
			}
		}
	}

	return nil
}

func haStatusReportCore(status string) error {

	var reportInfo reportHaInfo
	reportInfo.Status = status
	bytedata, err := json.Marshal(reportInfo)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]Post core haStatus err:", err)
		return err
	}

	url := fmt.Sprintf("/api/cpeConfig/cpes/%s/haStatus", public.G_coreConf.Sn)
	_, err = public.RequestCore(bytedata, public.G_coreConf.CoreAddress, public.G_coreConf.CorePort, public.G_coreConf.CoreProto, url)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]Post core haStatus err:", err, string(bytedata[:]), ", url:", url)
	} else {
		agentLog.AgentLogger.Info("Post core haStatus success. reportInfo: ", string(bytedata[:]))
	}

	return err
}

func monitorWanInfo(ctx context.Context) error {

	wanStatus := make(map[string]*WanStatus)
	haStatusActive := "UNKNOW"
	tick := time.NewTicker(time.Duration(wanMonitorInterval) * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			/* 从全局的 G_portConf 配置中获取wan接口信息，而不是每次从etcd中获取 */
			for _, dev := range public.G_portConf.WanConfig {

				phyif, err := public.GetPhyifName(dev.Device)
				if err != nil {
					agentLog.AgentLogger.Info("monitorWanInfo get phyif err: ", dev.Device)
					continue
				}

				ipType := "dhcp"
				if public.FileExists("/etc/debian_version") {
					// Debian
					filepath := "/etc/network/interfaces"
					if !public.FileExists("/etc/network/interfaces") {
						agentLog.AgentLogger.Info("monitorWanInfo file Notexist: ", filepath)
						continue
					}

					/* 从filepath中获取 BOOTPROTO 信息*/
					cmdstr := fmt.Sprintf("cat %s | grep iface | grep %s | awk '{print $4}'", filepath, phyif)
					err, result_str := public.ExecBashCmdWithRet(cmdstr)
					if err != nil {
						agentLog.AgentLogger.Info("monitorWanInfo filepath BOOTPROTO no found: ", filepath)
						continue
					}
					ipType = strings.Replace(result_str, "\n", "", -1)
				} else {
					// Default CentOS
					filepath := fmt.Sprintf("/etc/sysconfig/network-scripts/ifcfg-%s", phyif)
					if !public.FileExists(filepath) {
						agentLog.AgentLogger.Info("monitorWanInfo file Notexist: ", filepath)
						continue
					}
					/* 从filepath中获取 BOOTPROTO 信息*/
					cmdstr := fmt.Sprintf("cat %s | grep BOOTPROTO= | awk -F '=' '{print $2}'", filepath)
					err, result_str := public.ExecBashCmdWithRet(cmdstr)
					if err != nil {
						agentLog.AgentLogger.Info("monitorWanInfo filepath BOOTPROTO no found: ", filepath)
						continue
					}
					ipType = strings.Replace(result_str, "\n", "", -1)
				}

				/* 将上报信息改成大些 */
				if strings.ToLower(ipType) == "dhcp" {
					ipType = "DHCP"
				} else {
					ipType = "STATIC"
				}

				/* 从cpe系统中获取port的ip地址信息 */
				cmdstr := fmt.Sprintf("ip addr | grep %s | grep \"inet \"| awk 'NR==1' | awk '{print $2}'", phyif)
				err, result_str := public.ExecBashCmdWithRet(cmdstr)
				if err != nil {
					agentLog.AgentLogger.Info("monitorWanInfo ipAddr no found: ", phyif)
					continue
				}
				ipAddr := strings.Replace(result_str, "\n", "", -1)

				/* 从cpe系统中获取port的网关地址信息 */
				cmdstr = fmt.Sprintf("ip route | grep %s  | grep default | awk '{print $3}'", phyif)
				err, result_str = public.ExecBashCmdWithRet(cmdstr)
				if err != nil {
					agentLog.AgentLogger.Info("monitorWanInfo nexthop no found: ", phyif)
					continue
				}
				nexthop := strings.Replace(result_str, "\n", "", -1)

				if nexthop == "" || ipAddr == "" {
					/* 如果获取不到wan地址信息，则返回 */
					agentLog.AgentLogger.Info("monitorWanInfo nexthop or ip is null: ", phyif, " nexthop:", nexthop, "ip:", ipAddr)
					continue
				}

				if err := wanStatusUpdate(dev.Name, wanStatus, ipAddr, nexthop, ipType, phyif); err != nil {
					agentLog.AgentLogger.Info("[ERROR]wanStatusUpdate err : %v", err, "ID: ", dev.Name)
					continue
				}
			}

			// 上报控制器状态变化
			wanStatusReportCore(wanStatus)

			//check ha vip
			haCheckActive := "INACTIVE"
			enable, phyif, vipAddr := app.GetHaWanInfo()
			if enable {
				//如果开启HA
				cmd := fmt.Sprintf("ip addr | grep %s", phyif)
				if err, ret := public.ExecBashCmdWithRet(cmd); err != nil {
					agentLog.AgentLogger.Info("[ERROR]VipCheck faild: ", err, "cmd:", cmd, "Ret:", ret)
				} else {
					if strings.Contains(ret, "LOWER_UP") &&
						strings.Contains(ret, vipAddr) {
						haCheckActive = "ACTIVE"
					}
				}
			}

			if enable {
				if haCheckActive != haStatusActive {
					/* update to core haCheckActive */
					if err := haStatusReportCore(haCheckActive); err == nil {
						/* 记录 */
						haStatusActive = haCheckActive
					}
				}
			} else {
				haStatusActive = "UNKNOW"
			}

			/* 如果当前位HA运行状态，并且启用Local NAT */
			if haCheckActive == "ACTIVE" && app.GetLocalNatHa() {
				vipNew := strings.Split(string(vipAddr), "/")[0]
				if public.FileExists(haLocalNatPath) {
					/* 如果下发了，但是vip改变，则重新下发 */
					data, err := ioutil.ReadFile(haLocalNatPath) //read the file
					if err == nil {
						vipCur := strings.Split(string(data), "/")[0]
						phyifCur := strings.Split(string(data), "/")[1]
						if vipCur != vipNew || phyifCur != phyif {
							public.SetInterfaceSnatToSource(true, phyifCur, vipCur)
							public.SetInterfaceSnatToSource(false, phyif, vipNew)
							/* 重新记录新的vip地址到文件 */
							ioutil.WriteFile(haLocalNatPath, []byte(vipNew+"/"+phyif), 0644)
						}
					}
				} else {
					/* 如果策略没有下发，则下发nat策略 */
					public.SetInterfaceSnatToSource(false, phyif, vipNew)
					ioutil.WriteFile(haLocalNatPath, []byte(vipNew+"/"+phyif), 0644)
				}

			} else {
				/* 如果下发了，则删除nat策略 */
				if public.FileExists(haLocalNatPath) {
					data, err := ioutil.ReadFile(haLocalNatPath) //read the file
					if err == nil {
						vipCur := strings.Split(string(data), "/")[0]
						phyifCur := strings.Split(string(data), "/")[1]
						public.SetInterfaceSnatToSource(true, phyifCur, vipCur)
						/* 删除文件 */
						os.Remove(haLocalNatPath)
					}
				}
			}

		case <-ctx.Done():
			// 必须返回成功
			return nil
		}
	}
}

func initWanMonitor() error {
	// 监控wan接口地址和下一跳
	ctx, _ := context.WithCancel(context.Background())
	go func() {
		for {
			defer func() {
				if err := recover(); err != nil {
					/* 设置异常标志 */
					public.G_HeartBeatInfo.Status = "WARNING"
					agentLog.AgentLogger.Error("wan monitor panic err: %v", err)
				}
			}()

			if err := monitorWanInfo(ctx); err == nil {
				return
			} else {
				agentLog.AgentLogger.Error("wan monitor err :%v ", err)
			}
		}
	}()

	return nil
}
