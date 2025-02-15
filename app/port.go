package app

import (
	"cpeos/agentLog"
	"cpeos/config"
	"cpeos/etcd"
	"cpeos/public"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"

	"gitlab.daho.tech/gdaho/network"
	"gitlab.daho.tech/gdaho/util/derr"
)

const (
	PortNetworkScriptPath   = "/etc/sysconfig/network-scripts/ifcfg-%s"
	PortNetworkScriptFormat = `TYPE={{.Type}} 
BOOTPROTO=static 
NAME={{.PhyifName}}
DEVICE={{.PhyifName}} 
ONBOOT=yes 
{{- if ne .Address ""}}
IPADDR={{.Address}} 
{{- end}}
{{- if ne .Netmask ""}}
NETMASK={{.Netmask}} 
{{- end}}
`
)

/**
概述：Port用于定义cpe的物理接口实例。
**/

type PortConf struct {
	Id        string `json:"id"`        //(必填)物理接口的名称，例如：wan0，lan1等     //不可修改
	IpAddr    string `json:"ipAddr"`    //(必填)接口地址，带掩码                     //可以修改，但是wan0口不可以修改
	Nexthop   string `json:"nexthop"`   //(必填)接口网关，必须与IpAddr在同地址段内     //可以修改，但是wan0口不可以修改
	PhyifName string `json:"phyifName"` //{不填}记录物理接口名称
	Address   string `json:"address"`   //{不填}记录接口地址，不带掩码
	Netmask   string `json:"netmask"`   //{不填}记录地址掩码
	Type      string `json:"type"`      //{不填}记录接口类型
}

func getPortAddr(phyif string) (string, string) {
	ipAddr := ""
	nexthop := ""

	/* 从cpe系统中获取port的ip地址信息 */
	cmdstr := fmt.Sprintf("ip addr | grep %s | grep \"inet \"| awk 'NR==1' | awk '{print $2}'", phyif)
	err, result_str := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("getPortAddr ipAddr no found. phyif: ", phyif)
	} else {
		ipAddr = strings.Replace(result_str, "\n", "", -1)
		agentLog.AgentLogger.Info("getPortAddr ipAddr. phyif: ", phyif, ", nexthop: ", ipAddr)
	}

	/* 从cpe系统中获取port的网关地址信息 */
	cmdstr = fmt.Sprintf("ip route | grep %s  | grep default | awk '{print $3}'", phyif)
	err, result_str = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("getPortAddr nexthop no found. phyif: ", phyif)
	} else {
		nexthop = strings.Replace(result_str, "\n", "", -1)
		agentLog.AgentLogger.Info("getPortAddr nexthop. phyif: ", phyif, ", nexthop: ", nexthop)
	}

	return ipAddr, nexthop
}

func PortConfRebuild(fp *PortConf) (string, error) {

	var tmpl *template.Template
	var err error

	confFile := fmt.Sprintf(PortNetworkScriptPath, fp.PhyifName)
	tmpl, err = template.New("port").Parse(PortNetworkScriptFormat)
	if err != nil {
		return confFile, err
	}

	if public.FileExists(confFile) {
		err := os.Remove(confFile)
		if err != nil {
			return confFile, err
		}
	}

	fileConf, err := os.OpenFile(confFile, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
	if err != nil && !os.IsExist(err) {
		return confFile, err
	}
	defer fileConf.Close()

	if err = tmpl.Execute(fileConf, fp); err != nil {
		if public.FileExists(confFile) {
			os.Remove(confFile)
		}

		return confFile, err
	}

	return confFile, nil
}

func (conf *PortConf) Create(action int) error {

	var err error

	/* 获取port对应物理接口名称 */
	phyif, err := public.GetPhyifNameById(conf.Id)
	if err != nil {
		return err
	}

	/* 记录Port对应接口名称 */
	conf.PhyifName = phyif

	/* set link up */
	err = public.SetInterfaceLinkUp(phyif)
	if err != nil {
		return err
	}

	/* 关闭方向路由检查 */
	cmdstr := fmt.Sprintf("sysctl -w net.ipv4.conf.%s.rp_filter=0", phyif)
	err, _ = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info(cmdstr, err)
		return err
	}

	if strings.Contains(strings.ToLower(conf.Id), strings.ToLower("wan")) {
		if action == public.ACTION_ADD {
			/* 创建wan的时候，主动查询系统当前的wan口地址和网关信息 */
			/* 如果是重启，则跳过此步骤 */
			conf.IpAddr, conf.Nexthop = getPortAddr(phyif)
		}

		/* wan无需关心接口地址，直接返回*/
		return nil
	} else {
		if action == public.ACTION_ADD {
			/* 如果是lan端口，则需要生成系统network-scripts配置文件 */
			conf.Address = strings.Split(conf.IpAddr, "/")[0]
			lenth, _ := strconv.Atoi(strings.Split(conf.IpAddr, "/")[1])
			conf.Netmask = public.LenToSubnetMask(lenth)
			if strings.Contains(strings.ToLower(conf.PhyifName), "br") {
				conf.Type = "Bridge"
			} else {
				conf.Type = "Ethernet"
			}
			/* reload port configre */
			confFile, err := PortConfRebuild(conf)

			agentLog.AgentLogger.Info("Rebuild port file: ", confFile)
			if err != nil {
				agentLog.AgentLogger.Info("Rebuild port file failed ", confFile)
				return err
			}
		}
	}

	/* flush port address */
	if err = network.AddrFlush(conf.PhyifName); err != nil {
		agentLog.AgentLogger.Info("AddrFlush error ", conf.Id)
		return derr.Error{In: err.Error(), Out: "Destroy Port Error"}
	}

	/* set ip address */
	if conf.IpAddr != "" {
		err = public.SetInterfaceAddress(false, conf.PhyifName, conf.IpAddr)
		if err != nil {
			return err
		}
	}

	return nil
}

func (cfgCur *PortConf) Modify(cfgNew *PortConf) (error, bool) {

	var chg = false
	var err error

	if strings.Contains(strings.ToLower(cfgCur.Id), strings.ToLower("wan")) {
		/* wan口不更新配置 */
		return nil, false
	}

	if cfgCur.IpAddr != cfgNew.IpAddr {
		/* 如果是lan类型port实例，则更新地址。 */
		if cfgCur.IpAddr != "" {
			err = public.SetInterfaceAddress(true, cfgCur.PhyifName, cfgCur.IpAddr)
			if err != nil {
				return err, false
			}
		}

		if cfgNew.IpAddr != "" {
			err = public.SetInterfaceAddress(false, cfgCur.PhyifName, cfgNew.IpAddr)
			if err != nil {
				return err, false
			}
		}

		cfgCur.IpAddr = cfgNew.IpAddr
		chg = true
	}

	if cfgCur.Nexthop != cfgNew.Nexthop {
		/* 如果是lan类型port实例，则更新Nexthop。 */
		/* 更新static，check，subnet */
		UpdateNexthop(cfgCur, cfgNew.Nexthop, cfgNew.IpAddr)
		cfgCur.Nexthop = cfgNew.Nexthop
		chg = true
	}

	cfgNew.Address = strings.Split(cfgNew.IpAddr, "/")[0]
	lenth, _ := strconv.Atoi(strings.Split(cfgNew.IpAddr, "/")[1])
	cfgNew.Netmask = public.LenToSubnetMask(lenth)
	if strings.Contains(strings.ToLower(cfgCur.PhyifName), "br") {
		cfgNew.Type = "Bridge"
	} else {
		cfgNew.Type = "Ethernet"
	}
	if cfgCur.Address != cfgNew.Address || cfgCur.Netmask != cfgNew.Netmask || cfgCur.Type != cfgNew.Type {
		cfgCur.Address = cfgNew.Address
		cfgCur.Netmask = cfgNew.Netmask
		cfgCur.Type = cfgNew.Type

		/* reload port configre */
		confFile, err := PortConfRebuild(cfgCur)
		agentLog.AgentLogger.Info("Rebuild port file: ", confFile)
		if err != nil {
			agentLog.AgentLogger.Info("Rebuild port file failed ", confFile)
			return err, false
		}
		chg = true
	}

	return nil, chg
}

func (conf *PortConf) Destroy() error {

	if strings.Contains(strings.ToLower(conf.Id), strings.ToLower("wan")) {
		/* 如果是wan类型实例，则直接反馈成功。*/
		return nil
	}

	/* flush port address */
	if err := network.AddrFlush(conf.PhyifName); err != nil {
		agentLog.AgentLogger.Info("AddrFlush error ", conf.Id)
		return derr.Error{In: err.Error(), Out: "Destroy Port Error"}
	}

	/* remove port configre */
	conf.Address = ""
	conf.Netmask = ""
	confFile, err := PortConfRebuild(conf)
	agentLog.AgentLogger.Info("Remove port file: ", confFile)
	if err != nil {
		agentLog.AgentLogger.Info("Remove port file failed ", confFile)
		return err
	}

	return nil
}

func UpdateNexthop(port *PortConf, newNexthop, newIpAddr string) error {

	/* update subnet */
	UpdateSubnetNexhop(port, newNexthop)

	/* update static */
	UpdateStaticNexhop(port, newNexthop)

	/* update china route */
	if strings.Contains(strings.ToLower(port.Id), strings.ToLower("wan1")) {
		/* 如果是wan口，更新china路由 */
		UpdateChinaRoute(port, newNexthop, newIpAddr)

		/* 如果是wan口，重置resolv nameserver */
		UpdateResolvDns()

		/* 更新locla路由 */
		UpdateDnsDomainRule(port, newNexthop)
	}

	return nil
}

func GetPortInfoById(id string) (error, PortConf) {

	var find = false
	var port PortConf
	paths := []string{config.PortConfPath}
	ports, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.PortConfPath not found: ", err.Error())
	} else {
		for _, value := range ports {
			bytes := []byte(value)
			fp := &PortConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id != id {
				continue
			}

			port = *fp
			find = true
			break
		}
	}

	if !find {
		return derr.Error{In: err.Error(), Out: "PortNotFound"}, port
	}

	return nil, port
}

func GetPortNexthopById(id string) (string, string) {

	var nexthop = ""
	var devAddr = ""

	/* port */
	found := false
	paths := []string{config.PortConfPath}
	ports, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.PortConfPath not found: ", err.Error())
	} else {
		for _, value := range ports {
			bytes := []byte(value)
			fp := &PortConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id != id {
				continue
			}

			if fp.Nexthop != "" {
				nexthop = fp.Nexthop
			}
			devAddr = fp.IpAddr
			found = true
			break
		}
	}

	if found {
		return nexthop, devAddr
	}

	/* ipsec */
	found = false
	paths = []string{config.ConnConfPath}
	conns, err := etcd.EtcdGetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("config.ConnConfPath not found: ", err.Error())
	} else {
		for _, value := range conns {
			bytes := []byte(value)
			fp := &ConnConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if fp.Id != id {
				continue
			}

			nexthop = fp.Id
			devAddr = fp.LocalAddress
			found = true
			break
		}
	}

	return nexthop, devAddr
}
