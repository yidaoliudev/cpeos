package app

import (
	"cpeos/agentLog"
	"cpeos/config"
	"cpeos/etcd"
	"cpeos/public"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

/**
概述：Static用于定义CPE上的静态路由。

**/

type StaticConf struct {
	Id      string `json:"id"`      //(必填)id s
	Network string `json:"network"` //路由网段，带掩码
	Device  string `json:"device"`  //(必填)路由出口
	DevAddr string `json:"devAddr"` //{不填}记录出口IP地址
	Nexthop string `json:"nexthop"` //{不填}记录出口名称或者下一跳信息
}

func IpRouteBatchReplace(cidrs []string, device string, newDevice string) error {
	count := 0
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	for _, cidr := range cidrs {
		if cidr == "" {
			continue
		}
		count++
		cmd = append(cmd, cmdIpRoute(true, cidr, device)...)
		cmd = append(cmd, cmdIpRoute(false, cidr, newDevice)...)

		if count >= 50 {
			err := public.ExecBashCmd(strings.Join(cmd, " "))
			agentLog.AgentLogger.Info("IpRouteBatchReplace: ", cmd, " err:", err)

			/* 清空，重新开始 */
			count = 0
			cmd = []string{cmd_vtysh}
			cmd = append(cmd, cmdconfigTerm()...)
		}
	}
	/* 最后一遍保存 */
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("IpRouteBatchReplace: ", cmd, " err:", err)
	return err
}

func IpRouteBatch(undo bool, cidrs []string, device string) error {
	count := 0
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	for _, cidr := range cidrs {
		if cidr == "" {
			continue
		}
		count++
		cmd = append(cmd, cmdIpRoute(undo, cidr, device)...)
		if count >= 100 {
			err := public.ExecBashCmd(strings.Join(cmd, " "))
			agentLog.AgentLogger.Info("IpRouteBatch: ", cmd, " err:", err)

			/* 清空，重新开始 */
			count = 0
			cmd = []string{cmd_vtysh}
			cmd = append(cmd, cmdconfigTerm()...)
		}
	}
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("IpRouteBatch: ", cmd, " err:", err)
	return err
}

func IpRouteReplaceWithTable(destination string, device string, newDevice string, tableId int) (error, []string) {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdIpRouteWithTable(true, destination, device, tableId)...)
	cmd = append(cmd, cmdIpRouteWithTable(false, destination, newDevice, tableId)...)
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("IpRouteReplaceWithTable: ", cmd, " err:", err)
	return err, cmd
}

func IpRouteReplace(destination string, device string, newDevice string) (error, []string) {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdIpRoute(true, destination, device)...)
	cmd = append(cmd, cmdIpRoute(false, destination, newDevice)...)
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("IpRouteReplace: ", cmd, " err:", err)
	return err, cmd
}

func IpRoute(undo bool, destination string, device string) (error, []string) {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdIpRoute(undo, destination, device)...)
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("IpRoute: ", cmd, " err:", err)
	return err, cmd
}

func IpRouteWithTable(undo bool, destination string, device string, tableId int) (error, []string) {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdIpRouteWithTable(undo, destination, device, tableId)...)
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("IpRouteWithTable: ", cmd, " err:", err)
	return err, cmd
}

//根据用户输入的基础IP地址和CIDR掩码计算一个IP片段的区间
func GetIpSegRange(userSegIp, offset uint8) int {
	var ipSegMax uint8 = 255
	netSegIp := ipSegMax << offset
	segMinIp := netSegIp & userSegIp
	return int(segMinIp)
}

func GetIpSeg1Range(ipSegs []string, maskLen int) int {
	if maskLen > 8 {
		segIp, _ := strconv.Atoi(ipSegs[0])
		return segIp
	}
	ipSeg, _ := strconv.Atoi(ipSegs[0])
	return GetIpSegRange(uint8(ipSeg), uint8(8-maskLen))
}

func GetIpSeg2Range(ipSegs []string, maskLen int) int {
	if maskLen > 16 {
		segIp, _ := strconv.Atoi(ipSegs[1])
		return segIp
	}
	ipSeg, _ := strconv.Atoi(ipSegs[1])
	return GetIpSegRange(uint8(ipSeg), uint8(16-maskLen))
}

func GetIpSeg3Range(ipSegs []string, maskLen int) int {
	if maskLen > 24 {
		segIp, _ := strconv.Atoi(ipSegs[2])
		return segIp
	}
	ipSeg, _ := strconv.Atoi(ipSegs[2])
	return GetIpSegRange(uint8(ipSeg), uint8(24-maskLen))
}

func GetIpSeg4Range(ipSegs []string, maskLen int) int {
	ipSeg, _ := strconv.Atoi(ipSegs[3])
	segMinIp := GetIpSegRange(uint8(ipSeg), uint8(32-maskLen))
	return segMinIp
}

func GetCidrIpRange(cidr string) string {
	ip := strings.Split(cidr, "/")[0]
	ipSegs := strings.Split(ip, ".")
	maskLen, _ := strconv.Atoi(strings.Split(cidr, "/")[1])
	seg1MinIp := GetIpSeg1Range(ipSegs, maskLen)
	seg2MinIp := GetIpSeg2Range(ipSegs, maskLen)
	seg3MinIp := GetIpSeg3Range(ipSegs, maskLen)
	seg4MinIp := GetIpSeg4Range(ipSegs, maskLen)

	return strconv.Itoa(seg1MinIp) + "." + strconv.Itoa(seg2MinIp) + "." + strconv.Itoa(seg3MinIp) + "." + strconv.Itoa(seg4MinIp)
}

func AddRoute(undo bool, local string, destination string, device string) error {

	lanMaskLen := 32
	dstMaskLen := 32
	dstMaskLen2 := 32
	var dstAddrcidr string
	var dstAddrcidr2 string

	localAddrcidr := public.GetCidrIpRange(local)
	if strings.Contains(local, "/") {
		lanMaskLen, _ = strconv.Atoi(strings.Split(local, "/")[1])
	}

	if strings.Contains(destination, "/") {
		dstMaskLen, _ = strconv.Atoi(strings.Split(destination, "/")[1])
		dstAddrcidr = public.GetCidrIpRange(destination)
		dstMaskLen2 = dstMaskLen
		dstAddrcidr2 = dstAddrcidr
	} else {
		dstMaskLen = lanMaskLen
		dstAddrcidr = public.GetCidrIpRange(fmt.Sprintf("%s/%d", destination, dstMaskLen))
		dstMaskLen2 = 32
		dstAddrcidr2 = public.GetCidrIpRange(destination)
	}

	if localAddrcidr != dstAddrcidr || lanMaskLen != dstMaskLen {
		err, _ := IpRoute(undo, fmt.Sprintf("%s/%d", dstAddrcidr2, dstMaskLen2), device)
		if err != nil {
			agentLog.AgentLogger.Info("AddRoute error")
			return err
		}
	}

	return nil
}

func (conf *StaticConf) Create() error {

	nexthop, devAddr := GetPortNexthopById(strings.ToLower(conf.Device))
	if nexthop == "" {
		/* 如果没有下一跳信息，则不下发路由 */
		return nil
	}

	err, _ := IpRoute(false, conf.Network, nexthop)
	if err != nil {
		agentLog.AgentLogger.Info("AddRoute error")
		return err
	}

	conf.Nexthop = nexthop
	conf.DevAddr = devAddr
	return nil
}

func (cfgCur *StaticConf) Modify(cfgNew *StaticConf) (error, bool) {

	/* 检查路由的出接口是否有变化 */
	if cfgCur.Device != cfgNew.Device {
		nexthop, devAddr := GetPortNexthopById(strings.ToLower(cfgNew.Device))
		if nexthop == "" {
			/* 如果没有下一跳信息，则不下发路由 */
			if cfgCur.Nexthop != "" {
				/* 删除旧的路由 */
				err, _ := IpRoute(true, cfgCur.Network, cfgCur.Nexthop)
				if err != nil {
					agentLog.AgentLogger.Info("IpRoute del error")
					return err, false
				}
			}
			cfgCur.Device = cfgNew.Device
			cfgCur.Nexthop = nexthop
			cfgCur.DevAddr = devAddr
			return nil, true
		}

		if cfgCur.Nexthop == "" {
			/* 增加新路由 */
			err, _ := IpRoute(false, cfgCur.Network, nexthop)
			if err != nil {
				agentLog.AgentLogger.Info("IpRoute add error")
				return err, false
			}
		} else {
			/* 替换路由 */
			err, _ := IpRouteReplace(cfgCur.Network, cfgCur.Nexthop, nexthop)
			if err != nil {
				agentLog.AgentLogger.Info("IpRouteReplace error")
				return err, false
			}
		}
		cfgCur.Device = cfgNew.Device
		cfgCur.Nexthop = nexthop
		cfgCur.DevAddr = devAddr
		return nil, true
	}

	return nil, false
}

func (conf *StaticConf) Destroy() error {

	if conf.Nexthop == "" {
		return nil
	}

	err, _ := IpRoute(true, conf.Network, conf.Nexthop)
	if err != nil {
		agentLog.AgentLogger.Info("DelRoute error")
		return err
	}
	return nil
}

func UpdateStaticNexhop(port *PortConf, newNexthop string) error {
	paths := []string{config.StaticConfPath}
	statics, err := etcd.EtcdGetValues(paths)
	if err == nil {
		for _, value := range statics {
			bytes := []byte(value)
			fp := &StaticConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}
			if fp.Device != port.Id {
				continue
			}

			if fp.Nexthop == newNexthop {
				continue
			}

			/* 修改nexthop */
			if fp.Nexthop == "" {
				IpRoute(false, fp.Network, newNexthop)
			} else if newNexthop == "" {
				IpRoute(true, fp.Network, fp.Nexthop)
			} else {
				IpRouteReplace(fp.Network, fp.Nexthop, newNexthop)
			}

			/* 修改 etcd 数据 */
			fp.Nexthop = newNexthop
			fp.DevAddr = port.IpAddr
			saveData, err := json.Marshal(fp)
			if err != nil {
				agentLog.AgentLogger.Info("Marshal error " + config.StaticConfPath + fp.Id)
				continue
			}

			agentLog.AgentLogger.Info("Static etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.StaticConfPath+fp.Id, string(saveData[:]))
			if err != nil {
				agentLog.AgentLogger.Info("etcd save error " + config.StaticConfPath + fp.Id)
			}
		}
	}

	return nil
}

func GetStaticExtMembes() []string {

	var members []string
	paths := []string{config.StaticConfPath}
	statics, err := etcd.EtcdGetValues(paths)
	if err == nil {
		for _, value := range statics {
			bytes := []byte(value)
			fp := &StaticConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			lenth, _ := strconv.Atoi(strings.Split(fp.Network, "/")[1])
			if lenth != 32 {
				continue
			}

			members = append(members, fp.Network)
		}
	}

	return members
}
