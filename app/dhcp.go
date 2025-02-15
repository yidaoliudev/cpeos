package app

import (
	"cpeos/agentLog"
	"cpeos/public"
	"os"
	"strconv"
	"strings"
	"text/template"

	"gitlab.daho.tech/gdaho/util/derr"
)

const (
	dhcpdConfPath = "/etc/dhcp/dhcpd.conf"
	dhcpdStop     = "stop dhcpd"
	dhcpdStart    = "start dhcpd"
	dhcpdStatus   = "status dhcpd"
	dhcpdRestart  = "restart dhcpd"
)

const (
	DhcpServerConfFormat = `# Dhcpd configure
default-lease-time 600;
max-lease-time 7200;
{{- range .PortConfig}}
subnet {{.Subnet}} netmask {{.Netmask}} {
    range {{.RangeStart}} {{.RangeEnd}};
    {{- if ne .SecondaryDNS ""}}
    option domain-name-servers {{.PrimaryDNS}},{{.SecondaryDNS}};
    {{- else}}
    option domain-name-servers {{.PrimaryDNS}};
    {{- end}}
    option domain-name "local";
    option subnet-mask {{.Netmask}};
    option routers {{.Routers}};
    option broadcast-address {{.Broadcast}};
    {{- if ne .LeaseTime 0}}
    default-lease-time {{.LeaseTime}};
    {{- end}}
    {{- if ne .LeaseTimeMax 0}}
    max-lease-time {{.LeaseTimeMax}};
    {{- end}}
}
{{- end}}
`
)

type DhcpConf struct {
	Enable     bool           `json:"enable"`
	PortConfig []DhcpPortConf `json:"portNet"`
}

type DhcpPortConf struct {
	Id           string `json:"id"`           //(必填)
	IpAddr       string `json:"ipAddr"`       //(必填)
	RangeStart   string `json:"rangeStart"`   //(必填)
	RangeEnd     string `json:"rangeEnd"`     //(必填)
	PrimaryDNS   string `json:"primaryDNS"`   //(必填)
	SecondaryDNS string `json:"secondaryDNS"` //[选填]
	LeaseTime    int    `json:"leaseTime"`    //(必填)
	Subnet       string `json:"subnet"`       //{不填}
	Netmask      string `json:"netmask"`      //{不填}
	Routers      string `json:"routers"`      //{不填}
	Broadcast    string `json:"broadcast"`    //{不填}
	LeaseTimeMax int    `json:"leaseTimeMax"` //{不填}
}

func InitDhcpConf(fp *DhcpConf) error {

	iPoint := &fp.PortConfig
	for index := range fp.PortConfig {
		list := *iPoint

		list[index].Routers = strings.Split(list[index].IpAddr, "/")[0]
		lenth, _ := strconv.Atoi(strings.Split(list[index].IpAddr, "/")[1])
		list[index].Netmask = public.LenToSubnetMask(lenth)
		list[index].Subnet = public.GetCidrIpRange(list[index].IpAddr)
		list[index].Broadcast = public.GetIpBroadcast(list[index].Routers, list[index].Netmask)

	}

	return nil
}

func DhcpdConfRebuild(fp *DhcpConf) (string, error) {

	var tmpl *template.Template
	var err error

	confFile := dhcpdConfPath
	err = InitDhcpConf(fp)
	if err != nil {
		return confFile, derr.Error{In: err.Error(), Out: "dhcpConfInitError"}
	}

	tmpl, err = template.New("dhcpd").Parse(DhcpServerConfFormat)
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

func restartDhcpd() error {
	para := strings.Split(dhcpdRestart, " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}

	return nil
}

func stopDhcpd() error {
	para := strings.Split(dhcpdStop, " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}
	return nil
}

func (conf *DhcpConf) Create() error {

	if !conf.Enable {
		return nil
	}

	/* reload configre and restart dhcpd */
	confFile, err := DhcpdConfRebuild(conf)
	agentLog.AgentLogger.Info("Rebuild file: ", confFile)
	if err != nil {
		agentLog.AgentLogger.Info("Rebuild file failed ", confFile)
		return err
	}

	/* restart dhcpd */
	if err = restartDhcpd(); err != nil {
		agentLog.AgentLogger.Info("restartDhcpd failed", err)
		return err
	}

	return nil
}

func (cfgCur *DhcpConf) Modify(cfgNew *DhcpConf) (error, bool) {

	if cfgCur.Enable != cfgNew.Enable {
		agentLog.AgentLogger.Info("Dhcp Modify enable. cur: ", cfgCur.Enable, ", new: ", cfgNew.Enable)
		if !cfgNew.Enable {
			agentLog.AgentLogger.Info("Destroy dhcp.")
			return cfgNew.Destroy(), true
		} else {
			agentLog.AgentLogger.Info("Create dhcp.")
			return cfgNew.Create(), true
		}
	}

	/* 如果一直是关闭，则直接返回 */
	if !cfgNew.Enable {
		return nil, false
	}

	/* modfiy */
	/* 比较新旧配置是否一致 */
	chg := false
	for _, new := range cfgNew.PortConfig {
		found := false
		for _, old := range cfgCur.PortConfig {
			if new.Id == old.Id {
				found = true
				if new.IpAddr != old.IpAddr ||
					new.RangeStart != old.RangeStart ||
					new.RangeEnd != old.RangeEnd ||
					new.PrimaryDNS != old.PrimaryDNS ||
					new.SecondaryDNS != old.SecondaryDNS ||
					new.LeaseTime != old.LeaseTime {
					chg = true
					break
				}
			}
		}

		if !found || chg {
			chg = true
			break
		}
	}

	for _, old := range cfgNew.PortConfig {
		found := false
		for _, new := range cfgCur.PortConfig {
			if new.Id == old.Id {
				found = true
				if new.IpAddr != old.IpAddr ||
					new.RangeStart != old.RangeStart ||
					new.RangeEnd != old.RangeEnd ||
					new.PrimaryDNS != old.PrimaryDNS ||
					new.SecondaryDNS != old.SecondaryDNS ||
					new.LeaseTime != old.LeaseTime {
					chg = true
					break
				}
			}
		}

		if !found || chg {
			chg = true
			break
		}
	}

	if !chg {
		/* 没有信息改变 */
		return nil, false
	}

	/* reload configre */
	confFile, err := DhcpdConfRebuild(cfgNew)
	agentLog.AgentLogger.Info("Rebuild file: ", confFile)
	if err != nil {
		agentLog.AgentLogger.Info("Rebuild file failed ", confFile)
		return err, false
	}

	/* restart dhcpd */
	if err = restartDhcpd(); err != nil {
		agentLog.AgentLogger.Info("restartDhcpd failed", err)
		return err, false
	}

	return nil, true
}

func (conf *DhcpConf) Destroy() error {
	var err error

	/* stop dhcpd */
	if err = stopDhcpd(); err != nil {
		agentLog.AgentLogger.Info("stopDhcpd failed", err)
		return err
	}

	/* 删除配置文件
	confFile := dhcpdConfPath
	if public.FileExists(confFile) {
		agentLog.AgentLogger.Info("Remove file: ", confFile)
		err := os.Remove(confFile)
		if err != nil {
			agentLog.AgentLogger.Info("Remove file failed ", dhcpdConfPath)
			return err
		}
	}
	*/

	confFile, err := DhcpdConfRebuild(conf)
	agentLog.AgentLogger.Info("Rebuild file: ", confFile)
	if err != nil {
		agentLog.AgentLogger.Info("Rebuild file failed ", confFile)
		return err
	}

	return nil
}

func (fp *DhcpConf) Recover() error {

	/* 如果dhcpd服务没有启动，则直接退出 */
	if !fp.Enable {
		if err := stopDhcpd(); err != nil {
			agentLog.AgentLogger.Info("stopDhcpd failed", err)
		}
		return nil
	}

	/* restart dhcpd */
	if err := restartDhcpd(); err != nil {
		agentLog.AgentLogger.Info("restartDhcpd failed", err)
		return err
	}

	return nil
}
