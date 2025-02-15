package app

import (
	"cpeos/agentLog"
	"cpeos/config"
	"cpeos/etcd"
	"cpeos/public"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"text/template"
)

const (
	dnsNsPath         = "/home/smartdns/"
	dnsConfPath       = "/home/smartdns/smartdns.conf"
	dnsmasqResolvPath = "/etc/resolv.dnsmasq.conf"
	dnsGfwListPath    = "/home/gfwList.conf"
	stopSmartdns      = "stop smartdns"
	startSmartdns     = "start smartdns"
	enableSmartdns    = "enable smartdns"
	disableSmartdns   = "disable smartdns"
	restartSmartdns   = "restart smartdns"
	dnsResolvPath     = "/etc/resolv.conf"
	dnsResolvSavPath  = "/etc/resolv.conf.sav"
	domainRulePath    = "/home/smartdns/smartdns.d/domainaccRule_%s.conf"
	dnsRuleSdwanObj   = "dnsRuleSdwan"
	dnsRuleLocalObj   = "dnsRuleLocal"
	dnsRuleSdwanTable = 101   /* ip route table */
	dnsRuleLocalTable = 102   /* ip route table */
	dnsRuleSdwanPref  = 101   /* ip rule优先级 */
	dnsRuleLocalPref  = 102   /* ip rule优先级 */
	dnsRuleSdwanMask  = "101" /* iptables标签 */
	dnsRuleLocalMask  = "102" /* iptables标签 */
)

const (
	DnsServerConfFormat = `log-queries
log-facility=/var/log/smartdns.log
conf-dir=/home/smartdns/smartdns.d
resolv-file=/etc/resolv.dnsmasq.conf
listen-address={{.ListenAddress}},127.0.0.1
no-poll
no-hosts
cache-size=10000
neg-ttl=600
port=53
strict-order
`
	DomainRuleConfFormat = `
{{- range .Members}}
{{- if ne .SecondaryDNS ""}}
server=/{{.Domain}}/{{.SecondaryDNS}}
{{- end}}
{{- if ne .PrimaryDNS ""}}
server=/{{.Domain}}/{{.PrimaryDNS}}
{{- end}}
ipset=/{{.Domain}}/{{.IpsetObj}}
{{- end}}
`
)

type DomainMemberConf struct {
	Domain       string `json:"domain"`
	PrimaryDNS   string `json:"primaryDNS"`
	SecondaryDNS string `json:"secondaryDNS"`
	IpsetObj     string `json:"ipsetObj"`
}

type DomainRuleConf struct {
	Id      string
	Members []DomainMemberConf `json:"members"`
}

type AccRuleConf struct {
	Id           string   `json:"id"`           //ID
	Domains      []string `json:"domains"`      //加速域名
	PrimaryDNS   string   `json:"primaryDNS"`   //域名首选dns
	SecondaryDNS string   `json:"secondaryDNS"` //域名次选dns
	Networks     []string `json:"networks"`     //加速网段
	NatAddress   string   `json:"natAddress"`   //nat地址
	Device       string   `json:"device"`       //Sdwan Zone
}

type DnsConf struct {
	Enable        bool          `json:"enable"`
	FullMode      bool          `json:"fullMode"`
	ListenAddress string        `json:"listenAddress"`
	PrimaryDNS    string        `json:"primaryDNS"`
	SecondaryDNS  string        `json:"secondaryDNS"`
	SdwanConn     string        `json:"sdwanConn"`
	AccRules      []AccRuleConf `json:"accRules"`
}

func dnsGetServerConfPath() string {
	return dnsConfPath
}

func dnsGetDomainRulePath(id string) string {
	return fmt.Sprintf(domainRulePath, id)
}

func restartdns() error {
	var err error
	para := strings.Split(enableSmartdns, " ")
	if err = public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}

	para = strings.Split(restartSmartdns, " ")
	if err = public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}

	return nil
}

func stopdns() error {
	para := strings.Split(stopSmartdns, " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}

	para = strings.Split(disableSmartdns, " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}
	return nil
}

func resetdns() error {

	var err error

	/* 服务端配置变更重启smartdns进程 */
	if err = restartdns(); err != nil {
		agentLog.AgentLogger.Info("restartdns failed: ", err)
		return err
	}

	return nil
}

func DomainPolicyDestroy(obj string, nexthop string, mask string, table int, pref int) error {

	/* 删除 iptables rule */
	err := public.SetIptablesMask(true, obj, mask)
	if err != nil {
		agentLog.AgentLogger.Info("SetIptablesMask error")
		return err
	}

	/* 删除ip rule */
	err = public.SetIpRuleWithMark(true, mask, table, pref)
	if err != nil {
		agentLog.AgentLogger.Info("SetIpRuleWithMark error")
		return err
	}

	err, _ = IpRouteWithTable(true, "0.0.0.0/0", nexthop, table)
	if err != nil {
		agentLog.AgentLogger.Info("IpRouteWithTable error")
		return err
	}

	return nil
}

func DomainPolicyCreate(action int, obj string, nexthop string, mask string, table int, pref int) error {

	/* 创建ip rule */
	err := public.SetIpRuleWithMark(false, mask, table, pref)
	if err != nil {
		agentLog.AgentLogger.Info("SetIpRuleWithMark error")
		return err
	}

	if action == public.ACTION_ADD {
		err, _ = IpRouteWithTable(false, "0.0.0.0/0", nexthop, table)
		if err != nil {
			agentLog.AgentLogger.Info("IpRouteWithTable error")
			return err
		}
	}

	/* 创建iptables rule */
	err = public.SetIptablesMask(false, obj, mask)
	if err != nil {
		agentLog.AgentLogger.Info("SetIptablesMask error")
		return err
	}

	return nil
}

func AccRuleCreate(fp *AccRuleConf, action int) error {

	var tmpl *template.Template
	var err error
	ipsetObj := fmt.Sprintf("accRule_%s", fp.Id)

	/* Step1, 创建rule的ipset实例，默认7200秒timeout */
	err = public.IpsetObjCreateTimeout(ipsetObj, "hash:net", 1000000, 7200)
	if err != nil {
		agentLog.AgentLogger.Info("IpsetObjCreateTimeout error")
		return err
	}

	/* Step2, 往ipset实例增加member，主要有dns，network */
	if len(fp.Networks) > 0 {
		/* 将networks添加到ipsetObj, timeout 0 */
		for _, value := range fp.Networks {
			public.IpsetMemberTimeoutSet(false, ipsetObj, value, false, 0)
		}
	}
	if fp.PrimaryDNS != "" {
		public.IpsetMemberTimeoutSet(false, ipsetObj, fp.PrimaryDNS, false, 0)
	}
	if fp.SecondaryDNS != "" {
		public.IpsetMemberTimeoutSet(false, ipsetObj, fp.SecondaryDNS, false, 0)
	}
	if fp.NatAddress != "" {
		/* natAddress need nomatch */
		public.IpsetMemberTimeoutSet(false, ipsetObj, fp.NatAddress, true, 0)
	}

	/* Step3, 生成rule加速域名配置文件 */
	confFile := dnsGetDomainRulePath(fp.Id)
	if action == public.ACTION_ADD || !public.FileExists(confFile) {
		tmpl, err = template.New("domainRule").Parse(DomainRuleConfFormat)
		if err != nil {
			return err
		}

		if public.FileExists(confFile) {
			err := os.Remove(confFile)
			if err != nil {
				return err
			}
		}

		fileConf, err := os.OpenFile(confFile, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
		if err != nil && !os.IsExist(err) {
			return err
		}
		defer fileConf.Close()

		var domainRule DomainRuleConf
		domainRule.Id = fp.Id
		for _, value := range fp.Domains {
			var tmp DomainMemberConf
			tmp.Domain = value
			tmp.PrimaryDNS = fp.PrimaryDNS
			tmp.SecondaryDNS = fp.SecondaryDNS
			tmp.IpsetObj = ipsetObj
			domainRule.Members = append(domainRule.Members, tmp)
		}

		if err = tmpl.Execute(fileConf, &domainRule); err != nil {
			if public.FileExists(confFile) {
				os.Remove(confFile)
			}

			return err
		}
	}

	/* Step4, 通过iptables将ipset实例关联到sdwan-zone的Mask */
	err = public.SetIptablesMask(false, ipsetObj, dnsRuleSdwanMask)
	if err != nil {
		agentLog.AgentLogger.Info("SetIptablesMask error")
		return err
	}

	/* Step5, 配置snat策略，需要匹配接口+ipset实例 */
	if fp.NatAddress != "" {
		err = public.SetInterfaceAddress(false, "lo", fp.NatAddress)
		if err != nil {
			agentLog.AgentLogger.Info("SetInterfaceAddress faild.")
			//return err
		}

		err = public.SetInterfaceSnatToSourceByDst(false, fp.Device, ipsetObj, fp.NatAddress)
		if err != nil {
			return err
		}
	}

	return nil
}

func AccRuleDestroy(fp *AccRuleConf) error {

	var err error
	ipsetObj := fmt.Sprintf("accRule_%s", fp.Id)

	/* Step1, 删除rule加速域名配置文件 */
	confFile := dnsGetDomainRulePath(fp.Id)
	if public.FileExists(confFile) {
		err := os.Remove(confFile)
		if err != nil {
			return err
		}
	}

	/* Step2, 删除iptables */
	err = public.SetIptablesMask(true, ipsetObj, dnsRuleSdwanMask)
	if err != nil {
		agentLog.AgentLogger.Info("SetIptablesMask error")
		return err
	}

	/* Step3, 删除snat策略 */
	if fp.NatAddress != "" {
		err = public.SetInterfaceSnatToSourceByDst(true, fp.Device, ipsetObj, fp.NatAddress)
		if err != nil {
			return err
		}

		err = public.SetInterfaceAddress(true, "lo", fp.NatAddress)
		if err != nil {
			return err
		}
	}

	/* Step4, 删除rule的ipset实例 */
	err = public.IpsetObjDestroy(ipsetObj)
	if err != nil {
		agentLog.AgentLogger.Info("IpsetObjDestroy error")
		return err
	}

	return nil
}

func DnsConfCreate(fp *DnsConf) (string, error) {

	var tmpl *template.Template
	var err error

	confFile := dnsGetServerConfPath()
	tmpl, err = template.New("smartdns").Parse(DnsServerConfFormat)
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

func (conf *DnsConf) Create() error {

	var cmdstr string

	if !conf.Enable {
		return nil
	}

	/* 文件夹不存在 则创建 */
	if !public.FileExists(dnsNsPath) {
		/* 公共的配置文件 */
		cmdstr = fmt.Sprintf("mkdir -p %s/smartdns.d", dnsNsPath)
		if err, _ := public.ExecBashCmdWithRet(cmdstr); err != nil {
			agentLog.AgentLogger.Info(cmdstr, err)
			return err
		}
	}

	/* 生成conf 文件 */
	fileName, err := DnsConfCreate(conf)
	agentLog.AgentLogger.Info("DnsConfCreate: dnsConf: ", conf)
	if err != nil {
		agentLog.AgentLogger.Info("DnsConfCreate failed ", fileName)
		return err
	}

	/* 创建local ipset实例 */
	err = public.IpsetObjCreateTimeout(dnsRuleLocalObj, "hash:net", 1000000, 7200)
	if err != nil {
		agentLog.AgentLogger.Info("IpsetObjCreateTimeout error")
		return err
	}

	/* 创建sdwan ipset实例 */
	err = public.IpsetObjCreateTimeout(dnsRuleSdwanObj, "hash:net", 1000000, 7200)
	if err != nil {
		agentLog.AgentLogger.Info("IpsetObjCreateTimeout error")
		return err
	}

	/* 创建local policy */
	nexthop, _ := GetPortNexthopById("wan1")
	if nexthop != "" {
		err = DomainPolicyCreate(public.ACTION_ADD, dnsRuleLocalObj, nexthop, dnsRuleLocalMask, dnsRuleLocalTable, dnsRuleLocalPref)
		if err != nil {
			agentLog.AgentLogger.Info("DomainPolicyCreate local failed ")
			return err
		}
	}

	/* 创建sdwan policy */
	if conf.SdwanConn != "" {
		err = DomainPolicyCreate(public.ACTION_ADD, dnsRuleSdwanObj, conf.SdwanConn, dnsRuleSdwanMask, dnsRuleSdwanTable, dnsRuleSdwanPref)
		if err != nil {
			agentLog.AgentLogger.Info("DomainPolicyCreate sdwan failed ")
			return err
		}
	}

	/* 设置多出口加速策略 */
	if len(conf.AccRules) != 0 {
		for _, rule := range conf.AccRules {
			err = AccRuleCreate(&rule, public.ACTION_ADD)
			if err != nil {
				agentLog.AgentLogger.Info("AccRuleCreate failed, id: ", rule.Id)
				return err
			}
		}
	}

	/* set link up */
	err = public.SetInterfaceLinkUp("lo")
	if err != nil {
		return err
	}

	/* set ip address */
	if conf.ListenAddress != "" {
		err = public.SetInterfaceAddress(false, "lo", conf.ListenAddress)
		if err != nil {
			return err
		}
	}

	/* 设置iptable规则 */
	if conf.FullMode {
		if err := public.SetDnsDnatToDestination(false, conf.ListenAddress); err != nil {
			return err
		}
	}

	/* 生成dns-servers配置文件 */
	if conf.SecondaryDNS == "" {
		cmdstr = fmt.Sprintf("; generated by smartdns.service%snameserver %s%s", "\n", conf.PrimaryDNS, "\n")
	} else {
		cmdstr = fmt.Sprintf("; generated by smartdns.service%snameserver %s%snameserver %s%s", "\n", conf.PrimaryDNS, "\n", conf.SecondaryDNS, "\n")
	}
	err = ioutil.WriteFile(dnsmasqResolvPath, []byte(cmdstr), 0644)
	if err != nil {
		agentLog.AgentLogger.Info("dns reset dnsmasqResolvPath failed:", err)
		return nil
	}

	/* 重新生成resolv文件 */
	cmdstr = fmt.Sprintf("; generated by smartdns.service%snameserver %s%s", "\n", conf.ListenAddress, "\n")
	err = ioutil.WriteFile(dnsResolvPath, []byte(cmdstr), 0644)
	if err != nil {
		agentLog.AgentLogger.Info("dns reset resolv.conf failed:", err)
		return nil
	}

	/* 启动dns服务 */
	if err := resetdns(); err != nil {
		agentLog.AgentLogger.Info("dns Create: resetdns  failed: ", err)
		return err
	}

	return nil
}

func (cfgCur *DnsConf) Modify(cfgNew *DnsConf) (error, bool) {
	var serverChg = false
	var chg = false
	var cmdstr string

	if cfgCur.Enable != cfgNew.Enable {
		agentLog.AgentLogger.Info("DNS Modify. cur: ", cfgCur.Enable, ", new: ", cfgNew.Enable)
		if !cfgNew.Enable {
			agentLog.AgentLogger.Info("Destroy DNS.")
			return cfgCur.Destroy(), true
		} else {
			agentLog.AgentLogger.Info("Create DNS.")
			return cfgNew.Create(), true
		}
	}

	/* 如果一直是关闭，则直接返回 */
	if !cfgNew.Enable {
		return nil, false
	}

	/* modfiy */
	if cfgCur.ListenAddress != cfgNew.ListenAddress || cfgCur.FullMode != cfgNew.FullMode {
		/* 删除iptables规则 */
		if cfgCur.FullMode {
			if err := public.SetDnsDnatToDestination(true, cfgCur.ListenAddress); err != nil {
				return err, false
			}
		}

		if cfgCur.ListenAddress != cfgNew.ListenAddress {
			/* set ip address */
			if cfgCur.ListenAddress != "" {
				err := public.SetInterfaceAddress(true, "lo", cfgCur.ListenAddress)
				if err != nil {
					return err, false
				}
			}

			/* set ip address */
			if cfgNew.ListenAddress != "" {
				err := public.SetInterfaceAddress(false, "lo", cfgNew.ListenAddress)
				if err != nil {
					return err, false
				}
			}

			/* set dns resolv.conf */
			cmdstr = fmt.Sprintf("; generated by smartdns.service%snameserver %s%s", "\n", cfgNew.ListenAddress, "\n")
			err := ioutil.WriteFile(dnsResolvPath, []byte(cmdstr), 0644)
			if err != nil {
				agentLog.AgentLogger.Info("dns reset resolv.conf failed:", err)
				return err, false
			}
		}

		/* 增加iptables规则 */
		if cfgNew.FullMode {
			if err := public.SetDnsDnatToDestination(false, cfgNew.ListenAddress); err != nil {
				return err, false
			}
		}

		cfgCur.ListenAddress = cfgNew.ListenAddress
		cfgCur.FullMode = cfgNew.FullMode
		chg = true
		serverChg = true
	}

	if cfgCur.PrimaryDNS != cfgNew.PrimaryDNS {
		cfgCur.PrimaryDNS = cfgNew.PrimaryDNS
		chg = true
		serverChg = true
	}

	if cfgCur.SecondaryDNS != cfgNew.SecondaryDNS {
		cfgCur.SecondaryDNS = cfgNew.SecondaryDNS
		chg = true
		serverChg = true
	}

	if cfgCur.SdwanConn != cfgNew.SdwanConn {
		if cfgCur.SdwanConn != "" && cfgNew.SdwanConn != "" {
			/* 修改下一跳 */
			IpRouteReplaceWithTable("0.0.0.0/0", cfgCur.SdwanConn, cfgNew.SdwanConn, dnsRuleSdwanTable)
		} else if cfgCur.SdwanConn != "" {
			err := DomainPolicyDestroy(dnsRuleSdwanObj, cfgCur.SdwanConn, dnsRuleSdwanMask, dnsRuleSdwanTable, dnsRuleSdwanPref)
			if err != nil {
				agentLog.AgentLogger.Info("DomainPolicyDestroy sdwan failed ")
				return err, false
			}
		} else {
			err := DomainPolicyCreate(public.ACTION_ADD, dnsRuleSdwanObj, cfgNew.SdwanConn, dnsRuleSdwanMask, dnsRuleSdwanTable, dnsRuleSdwanPref)
			if err != nil {
				agentLog.AgentLogger.Info("DomainPolicyCreate sdwan failed ")
				return err, false
			}
		}

		cfgCur.SdwanConn = cfgNew.SdwanConn
		chg = true
		//serverChg = true
	}

	/* 比较新旧配置是否一致 */
	accChg := false
	if len(cfgNew.AccRules) != len(cfgCur.AccRules) {
		accChg = true
	} else {
		for _, new := range cfgNew.AccRules {
			found := false
			for _, old := range cfgCur.AccRules {
				if new.Id == old.Id {
					if new.Device != old.Device ||
						new.PrimaryDNS != old.PrimaryDNS ||
						new.SecondaryDNS != old.SecondaryDNS ||
						new.NatAddress != old.NatAddress {
						accChg = true
					}

					add, delete := public.Arrcmp(old.Domains, new.Domains)
					if len(add) != 0 || len(delete) != 0 {
						accChg = true
					}

					add, delete = public.Arrcmp(old.Networks, new.Networks)
					if len(add) != 0 || len(delete) != 0 {
						accChg = true
					}

					found = true
					break
				}
			}
			if !found {
				accChg = true
			}
		}

		for _, old := range cfgCur.AccRules {
			found := false
			for _, new := range cfgNew.AccRules {
				if new.Id == old.Id {
					if new.Device != old.Device ||
						new.PrimaryDNS != old.PrimaryDNS ||
						new.SecondaryDNS != old.SecondaryDNS ||
						new.NatAddress != old.NatAddress {
						accChg = true
					}

					add, delete := public.Arrcmp(old.Domains, new.Domains)
					if len(add) != 0 || len(delete) != 0 {
						accChg = true
					}

					add, delete = public.Arrcmp(old.Networks, new.Networks)
					if len(add) != 0 || len(delete) != 0 {
						accChg = true
					}

					found = true
					break
				}
			}
			if !found {
				accChg = true
			}
		}
	}

	if accChg {
		/* 全量更新 */
		for _, old := range cfgCur.AccRules {
			err := AccRuleDestroy(&old)
			if err != nil {
				agentLog.AgentLogger.Info("AccRuleDestroy failed, id: ", old.Id)
				return err, false
			}
		}

		for _, new := range cfgNew.AccRules {
			err := AccRuleCreate(&new, public.ACTION_ADD)
			if err != nil {
				agentLog.AgentLogger.Info("AccRuleCreate failed, id: ", new.Id)
				return err, false
			}
		}

		chg = true
		serverChg = true
	}

	if serverChg {
		/* 重新生成conf 文件 */
		_, err := DnsConfCreate(cfgCur)
		agentLog.AgentLogger.Info("DnsConfRebuild: dnsConf: ", cfgCur)
		if err != nil {
			agentLog.AgentLogger.Info("DnsConfRebuild failed ")
			return err, false
		}

		/* 重新生成dns-servers配置文件 */
		if cfgCur.SecondaryDNS == "" {
			cmdstr = fmt.Sprintf("; generated by smartdns.service%snameserver %s%s", "\n", cfgCur.PrimaryDNS, "\n")
		} else {
			cmdstr = fmt.Sprintf("; generated by smartdns.service%snameserver %s%snameserver %s%s", "\n", cfgCur.PrimaryDNS, "\n", cfgCur.SecondaryDNS, "\n")
		}
		err = ioutil.WriteFile(dnsmasqResolvPath, []byte(cmdstr), 0644)
		if err != nil {
			agentLog.AgentLogger.Info("dns reset dnsmasqResolvPath failed:", err)
			return err, false
		}

		/* 重启dns服务 */
		if err := resetdns(); err != nil {
			agentLog.AgentLogger.Info("Recover : resetdns  failed: ", err)
			return err, false
		}
	}

	return nil, chg
}

func (conf *DnsConf) Destroy() error {

	/* 关闭dns服务 */
	if err := stopdns(); err != nil {
		return err
	}

	/* 删除多出口加速策略 */
	if len(conf.AccRules) != 0 {
		for _, rule := range conf.AccRules {
			err := AccRuleDestroy(&rule)
			if err != nil {
				agentLog.AgentLogger.Info("AccRuleDestroy failed, id: ", rule.Id)
				return err
			}
		}
	}

	nexthop, _ := GetPortNexthopById("wan1")
	if nexthop != "" {
		err := DomainPolicyDestroy(dnsRuleLocalObj, nexthop, dnsRuleLocalMask, dnsRuleLocalTable, dnsRuleLocalPref)
		if err != nil {
			agentLog.AgentLogger.Info("DomainPolicyDestroy local failed ")
			return err
		}
	}

	if conf.SdwanConn != "" {
		err := DomainPolicyDestroy(dnsRuleSdwanObj, conf.SdwanConn, dnsRuleSdwanMask, dnsRuleSdwanTable, dnsRuleSdwanPref)
		if err != nil {
			agentLog.AgentLogger.Info("DomainPolicyDestroy sdwan failed ")
			return err
		}
	}

	/* 删除local ipset实例 */
	err := public.IpsetObjDestroy(dnsRuleLocalObj)
	if err != nil {
		agentLog.AgentLogger.Info("IpsetObjDestroy error")
		return err
	}

	/* 删除sdwan ipset实例 */
	err = public.IpsetObjDestroy(dnsRuleSdwanObj)
	if err != nil {
		agentLog.AgentLogger.Info("IpsetObjDestroy error")
		return err
	}

	/* 删除iptables规则 */
	if conf.FullMode {
		if err := public.SetDnsDnatToDestination(true, conf.ListenAddress); err != nil {
			return err
		}
	}

	/* set ip address */
	if conf.ListenAddress != "" {
		err := public.SetInterfaceAddress(true, "lo", conf.ListenAddress)
		if err != nil {
			return err
		}
	}

	/* 删除备份resolv文件*/
	if public.FileExists(dnsResolvSavPath) {
		err = os.Remove(dnsResolvSavPath)
		if err != nil {
			agentLog.AgentLogger.Info("Remove dnsResolvSavPath file failed, err:", err.Error())
		}
	}

	/* 删除dns-servers配置文件 */
	if public.FileExists(dnsmasqResolvPath) {
		err = os.Remove(dnsmasqResolvPath)
		if err != nil {
			agentLog.AgentLogger.Info("Remove dnsmasqResolvPath file failed, err:", err.Error())
		}
	}

	/* 重新生成默认resolv文件 */
	cmdstr := fmt.Sprintf("; generated by cpe.service%snameserver %s%snameserver %s%s", "\n", "223.5.5.5", "\n", "114.114.114.114", "\n")
	err = ioutil.WriteFile(dnsResolvPath, []byte(cmdstr), 0644)
	if err != nil {
		agentLog.AgentLogger.Info("dns reset resolv.conf failed:", err)
		return nil
	}

	return nil
}

func (fp *DnsConf) Recover() error {
	var err error
	var cmdstr string

	/* 如果dns服务没有启动，则直接退出 */
	if !fp.Enable {
		return nil
	}

	/* 创建local ipset实例 */
	err = public.IpsetObjCreateTimeout(dnsRuleLocalObj, "hash:net", 1000000, 7200)
	if err != nil {
		agentLog.AgentLogger.Info("IpsetObjCreateTimeout error")
		return err
	}

	/* 创建sdwan ipset实例 */
	err = public.IpsetObjCreateTimeout(dnsRuleSdwanObj, "hash:net", 1000000, 7200)
	if err != nil {
		agentLog.AgentLogger.Info("IpsetObjCreateTimeout error")
		return err
	}

	/* 创建local policy */
	nexthop, _ := GetPortNexthopById("wan1")
	if nexthop != "" {
		err = DomainPolicyCreate(public.ACTION_RECOVER, dnsRuleLocalObj, nexthop, dnsRuleLocalMask, dnsRuleLocalTable, dnsRuleLocalPref)
		if err != nil {
			agentLog.AgentLogger.Info("DomainPolicyCreate local failed ")
			return err
		}
	}

	/* 创建sdwan policy */
	if fp.SdwanConn != "" {
		err = DomainPolicyCreate(public.ACTION_RECOVER, dnsRuleSdwanObj, fp.SdwanConn, dnsRuleSdwanMask, dnsRuleSdwanTable, dnsRuleSdwanPref)
		if err != nil {
			agentLog.AgentLogger.Info("DomainPolicyCreate sdwan failed ")
			return err
		}
	}

	/* 设置多出口加速策略 */
	if len(fp.AccRules) != 0 {
		for _, rule := range fp.AccRules {
			err = AccRuleCreate(&rule, public.ACTION_RECOVER)
			if err != nil {
				agentLog.AgentLogger.Info("AccRuleCreate failed, id: ", rule.Id)
				return err
			}
		}
	}

	/* set link up */
	err = public.SetInterfaceLinkUp("lo")
	if err != nil {
		return err
	}

	/* set ip address */
	if fp.ListenAddress != "" {
		err := public.SetInterfaceAddress(false, "lo", fp.ListenAddress)
		if err != nil {
			return err
		}
	}

	/* 设置iptable规则 */
	if fp.FullMode {
		if err = public.SetDnsDnatToDestination(false, fp.ListenAddress); err != nil {
			return err
		}
	}

	/* 重新生成dns-servers配置文件 */
	if fp.SecondaryDNS == "" {
		cmdstr = fmt.Sprintf("; generated by smartdns.service%snameserver %s%s", "\n", fp.PrimaryDNS, "\n")
	} else {
		cmdstr = fmt.Sprintf("; generated by smartdns.service%snameserver %s%snameserver %s%s", "\n", fp.PrimaryDNS, "\n", fp.SecondaryDNS, "\n")
	}
	err = ioutil.WriteFile(dnsmasqResolvPath, []byte(cmdstr), 0644)
	if err != nil {
		agentLog.AgentLogger.Info("dns reset dnsmasqResolvPath failed:", err)
		return nil
	}

	/* 重新生成resolv文件 */
	cmdstr = fmt.Sprintf("; generated by smartdns.service%snameserver %s%s", "\n", fp.ListenAddress, "\n")
	err = ioutil.WriteFile(dnsResolvPath, []byte(cmdstr), 0644)
	if err != nil {
		agentLog.AgentLogger.Info("dns reset resolv.conf failed:", err)
		return nil
	}

	/* 重启dns服务 */
	if err = resetdns(); err != nil {
		agentLog.AgentLogger.Info("Recover : resetdns  failed: ", err)
		return err
	}

	return nil
}

func UpdateResolvDns() error {
	value, err := etcd.EtcdGetValue(config.DnsConfPath)
	if err != nil {
		agentLog.AgentLogger.Info("dns not found: ", err.Error())
		return nil
	}

	bytes := []byte(value)
	fp := &DnsConf{}
	err = json.Unmarshal(bytes, fp)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]dns data unmarshal failed: ", err.Error())
		return nil
	}

	if fp.Enable {
		cmdstr := fmt.Sprintf("; generated by smartdns.service%snameserver %s%s", "\n", fp.ListenAddress, "\n")
		err = ioutil.WriteFile(dnsResolvPath, []byte(cmdstr), 0644)
		if err != nil {
			agentLog.AgentLogger.Info("dns reset resolv.conf failed:", err)
			return nil
		}
	}

	return nil
}

func UpdateDnsDomainRule(port *PortConf, newNexthop string) error {

	value, err := etcd.EtcdGetValue(config.DnsConfPath)
	if err != nil {
		agentLog.AgentLogger.Info("dns not found: ", err.Error())
		return nil
	}

	bytes := []byte(value)
	fp := &DnsConf{}
	err = json.Unmarshal(bytes, fp)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]dns data unmarshal failed: ", err.Error())
		return nil
	}

	if fp.Enable {
		if port.Nexthop == "" {
			/* 新建 */
			DomainPolicyCreate(public.ACTION_ADD, dnsRuleLocalObj, newNexthop, dnsRuleLocalMask, dnsRuleLocalTable, dnsRuleLocalPref)
		} else {
			/* 修改下一跳 */
			IpRouteReplaceWithTable("0.0.0.0/0", port.Nexthop, newNexthop, dnsRuleLocalTable)
		}
	}

	return nil
}
