package app

import (
	"cpeos/agentLog"
	"cpeos/config"
	"cpeos/etcd"
	"cpeos/public"
	"encoding/json"
	"os"
	"strings"

	"text/template"
)

const (
	HaRole_Master = 1
	HaRole_Backup = 2
)

/**
概述：CPE Ha模块，使用vrrp开源方式实现。

HA 判断为正常，需要满足所有Port实例的状态都UP，如果启用健康检查，则健康检查为UP。
HA 状态为运行备时，需要停止strongswan服务，只有运行主上启用strongswan。

**/

type HaConf struct {
	Enable     bool         `json:"enable"`  //HA开关
	Role       int          `json:"role"`    //(必填)HA角色
	PortConfig []HaPortConf `json:"portVip"` //(必填)HA接口配置
}

type HaPortConf struct {
	Id        string   `json:"id"`        //(必填)物理接口的名称，例如：wan0，lan1等  //不可修改
	VipAddr   string   `json:"ipAddr"`    //(必填)接口Vip，HA时需要配置，不带掩码     //可以修改
	Peer      string   `json:"peer"`      //(必填)对端接口地址
	Networks  []string `json:"networks"`  //(必填)使用接口地址访问的网段，主要有网关地址、core、webconsole，用于下发iptables rule
	PhyifName string   `json:"phyifName"` //{不填}记录物理接口名称
	Address   string   `json:"address"`   //{不填}记录接口的地址
}

const (
	keepalivedConfPath = "/etc/keepalived/keepalived.conf"
	keepalivedStop     = "stop keepalived"
	keepalivedStart    = "start keepalived"
	keepalivedStatus   = "status keepalived"
	keepalivedRestart  = "restart keepalived"
	sysctl_cmd         = "systemctl"
)

const (
	KeepalivedMasterConfFormat = `global_defs {
    ikelifetime=LVS_MASTER
}
vrrp_script checkhaproxy {
    script "/etc/keepalived/chk_haproxy.sh"
    interval 1
    weight -20
}
{{- range .PortConfig}}
vrrp_instance VI_{{.PhyifName}} {
    state MASTER
    interface {{.PhyifName}}
    virtual_router_id 1
    priority 100
    advert_int 1
    authentication {
        auth_type PASS
        auth_pass 123
    }
    unicast_src_ip {{.Address}}
    unicast_peer {
        {{.Peer}}
    }
    virtual_ipaddress {
        {{.VipAddr}}
    }
    track_script {
        checkhaproxy
    }
}
{{- end}}
`
	KeepalivedBackupConfFormat = `global_defs {
    ikelifetime=LVS_BACKUP
}
vrrp_script checkhaproxy {
    script "/etc/keepalived/chk_haproxy.sh"
    interval 1
    weight -20
}
{{- range .PortConfig}}
vrrp_instance VI_{{.PhyifName}} {
    state BACKUP
    interface {{.PhyifName}}
    virtual_router_id 1
    priority 90
    advert_int 1
    authentication {
        auth_type PASS
        auth_pass 123
    }
    unicast_src_ip {{.Address}}
    unicast_peer {
        {{.Peer}}
    }
    virtual_ipaddress {
        {{.VipAddr}}
    }
    track_script {
        checkhaproxy
    }
}
{{- end}}
`
)

func KeepalivedConfCreate(fp *HaConf) (string, error) {

	var tmpl *template.Template
	var err error

	confFile := keepalivedConfPath
	if fp.Role == HaRole_Master {
		tmpl, err = template.New("keepalived").Parse(KeepalivedMasterConfFormat)
	} else {
		tmpl, err = template.New("keepalived").Parse(KeepalivedBackupConfFormat)
	}
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

func restartKeepalived() error {
	para := strings.Split(keepalivedRestart, " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}

	return nil
}

func stopKeepalived() error {
	para := strings.Split(keepalivedStop, " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}
	return nil
}

func (conf *HaConf) Create() error {

	if !conf.Enable {
		return nil
	}

	/* 检查校验，根据配置文件中的逻辑接口查找实际物理接口 */
	iPoint := &conf.PortConfig
	for index := range conf.PortConfig {
		list := *iPoint
		/*
			phyifName, err := public.GetPhyifNameById(list[index].Id)
			if err != nil {
				agentLog.AgentLogger.Info("Ha GetPhyifNameById faild: ", list[index].Id)
				return err
			}
			agentLog.AgentLogger.Info("Ha GetPhyifNameById, ", list[index].Id, ":", phyifName)
			list[index].PhyifName = phyifName
		*/
		err, port := GetPortInfoById(list[index].Id)
		if err != nil {
			agentLog.AgentLogger.Info("Ha GetPortInfoById faild: ", list[index].Id)
			return err
		}
		list[index].Address = strings.Split(port.IpAddr, "/")[0]
		list[index].PhyifName = port.PhyifName

		/* 设置iptables 规则 */
		for _, network := range list[index].Networks {
			if list[index].Address != "" && network != "" && list[index].PhyifName != "" {
				err = public.SetInterfaceSnatToSourceByNetwork(false, list[index].PhyifName, network, list[index].Address)
				if err != nil {
					agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", list[index].Id)
					return err
				}
			}
		}

		if list[index].Peer != "" {
			err = public.SetInterfaceSnatToSourceByNetwork(false, list[index].PhyifName, list[index].Peer, list[index].Address)
			if err != nil {
				agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", list[index].Id)
				return err
			}
		}
	}

	/* reload configre and restart keeplived */
	/* 创建配置配置文件 */
	confFile, err := KeepalivedConfCreate(conf)
	agentLog.AgentLogger.Info("Create file: ", confFile)
	if err != nil {
		agentLog.AgentLogger.Info("Create file failed ", confFile)
		return err
	}

	/* restart keeplived */
	if err = restartKeepalived(); err != nil {
		agentLog.AgentLogger.Info("restartKeepalived failed", err)
		return err
	}

	return nil
}

func (cfgCur *HaConf) Modify(cfgNew *HaConf) (error, bool) {

	if cfgCur.Enable != cfgNew.Enable {
		agentLog.AgentLogger.Info("HA Modify enable. cur: ", cfgCur.Enable, ", new: ", cfgNew.Enable)
		if !cfgNew.Enable {
			agentLog.AgentLogger.Info("Destroy HA.")
			return cfgCur.Destroy(), true
		} else {
			agentLog.AgentLogger.Info("Create HA.")
			return cfgNew.Create(), true
		}
	}

	/* 如果一直是关闭，则直接返回 */
	if !cfgNew.Enable {
		return nil, false
	}

	/* modfiy */
	iPoint := &cfgNew.PortConfig
	for index := range cfgNew.PortConfig {
		list := *iPoint
		/*
			phyifName, err := public.GetPhyifNameById(list[index].Id)
			if err != nil {
				agentLog.AgentLogger.Info("Ha GetPhyifNameById faild: ", list[index].Id)
				return err, false
			}
			list[index].PhyifName = phyifName
		*/
		err, port := GetPortInfoById(list[index].Id)
		if err != nil {
			agentLog.AgentLogger.Info("Ha GetPortInfoById faild: ", list[index].Id)
			return err, false
		}
		list[index].Address = strings.Split(port.IpAddr, "/")[0]
		list[index].PhyifName = port.PhyifName
	}

	chg := false
	serverChg := false
	if cfgCur.Role != cfgNew.Role {
		/* 检查校验，根据配置文件中的逻辑接口查找实际物理接口 */
		agentLog.AgentLogger.Info("HA Modify role. cur: ", cfgCur.Role, ", new: ", cfgNew.Role)
		serverChg = true
		chg = true
	} else {
		/* 比较新旧配置是否一致 */
		if len(cfgNew.PortConfig) != len(cfgCur.PortConfig) {
			serverChg = true
			chg = true
		} else {
			for _, new := range cfgNew.PortConfig {
				found := false
				for _, old := range cfgCur.PortConfig {
					if new.Id == old.Id {
						if new.VipAddr != old.VipAddr ||
							new.Address != old.Address ||
							new.Peer != old.Peer ||
							new.PhyifName != old.PhyifName {
							serverChg = true
							chg = true
						} else {
							add, delete := public.Arrcmp(new.Networks, old.Networks)
							if len(add) != 0 || len(delete) != 0 {
								///serverChg = true
								chg = true
							}
						}
						found = true
						break
					}
				}
				if !found {
					serverChg = true
					chg = true
				}
			}

			for _, old := range cfgCur.PortConfig {
				found := false
				for _, new := range cfgNew.PortConfig {
					if new.Id == old.Id {
						if new.VipAddr != old.VipAddr ||
							new.Address != old.Address ||
							new.Peer != old.Peer ||
							new.PhyifName != old.PhyifName {
							serverChg = true
							chg = true
						} else {
							add, delete := public.Arrcmp(new.Networks, old.Networks)
							if len(add) != 0 || len(delete) != 0 {
								///serverChg = true
								chg = true
							}
						}
						found = true
						break
					}
				}
				if !found {
					serverChg = true
					chg = true
				}
			}
		}

		if !chg {
			/* 没有信息改变 */
			return nil, false
		}
	}

	/* update iptables snat rules. */
	for _, old := range cfgCur.PortConfig {
		found := false
		for _, new := range cfgNew.PortConfig {
			if new.Id == old.Id {
				if new.Address != old.Address ||
					new.PhyifName != old.PhyifName {
					/* delete old */
					for _, network := range old.Networks {
						if old.Address != "" && network != "" && old.PhyifName != "" {
							err := public.SetInterfaceSnatToSourceByNetwork(true, old.PhyifName, network, old.Address)
							if err != nil {
								agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", old.Id)
								return err, false
							}
						}
					}
					if old.Peer != "" {
						err := public.SetInterfaceSnatToSourceByNetwork(true, old.PhyifName, old.Peer, old.Address)
						if err != nil {
							agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", old.Id)
							return err, false
						}
					}

					/* add new */
					for _, network := range new.Networks {
						if new.Address != "" && network != "" && new.PhyifName != "" {
							err := public.SetInterfaceSnatToSourceByNetwork(false, new.PhyifName, network, new.Address)
							if err != nil {
								agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", new.Id)
								return err, false
							}
						}
					}
					if new.Peer != "" {
						err := public.SetInterfaceSnatToSourceByNetwork(false, new.PhyifName, new.Peer, new.Address)
						if err != nil {
							agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", new.Id)
							return err, false
						}
					}
				} else {
					add, delete := public.Arrcmp(old.Networks, new.Networks)
					if len(add) != 0 {
						for _, network := range add {
							if old.Address != "" && network != "" && old.PhyifName != "" {
								err := public.SetInterfaceSnatToSourceByNetwork(false, old.PhyifName, network, old.Address)
								if err != nil {
									agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", old.Id)
									return err, false
								}
							}
						}
					}

					if len(delete) != 0 {
						for _, network := range delete {
							if old.Address != "" && network != "" && old.PhyifName != "" {
								err := public.SetInterfaceSnatToSourceByNetwork(true, old.PhyifName, network, old.Address)
								if err != nil {
									agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", old.Id)
									return err, false
								}
							}
						}
					}

					if new.Peer != old.Peer {
						if old.Peer != "" {
							err := public.SetInterfaceSnatToSourceByNetwork(true, old.PhyifName, old.Peer, old.Address)
							if err != nil {
								agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", old.Id)
								return err, false
							}
						}

						if new.Peer != "" {
							err := public.SetInterfaceSnatToSourceByNetwork(false, old.PhyifName, new.Peer, old.Address)
							if err != nil {
								agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", old.Id)
								return err, false
							}
						}
					}
				}

				found = true
				break
			}
		}

		if !found {
			for _, network := range old.Networks {
				if old.Address != "" && network != "" && old.PhyifName != "" {
					err := public.SetInterfaceSnatToSourceByNetwork(true, old.PhyifName, network, old.Address)
					if err != nil {
						agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", old.Id)
						return err, false
					}
				}
			}

			if old.Peer != "" {
				err := public.SetInterfaceSnatToSourceByNetwork(true, old.PhyifName, old.Peer, old.Address)
				if err != nil {
					agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", old.Id)
					return err, false
				}
			}
		}
	}

	for _, new := range cfgNew.PortConfig {
		found := false
		for _, old := range cfgCur.PortConfig {
			if new.Id == old.Id {
				found = true
				break
			}
		}
		if !found {
			for _, network := range new.Networks {
				if new.Address != "" && network != "" && new.PhyifName != "" {
					err := public.SetInterfaceSnatToSourceByNetwork(false, new.PhyifName, network, new.Address)
					if err != nil {
						agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", new.Id)
						return err, false
					}
				}
			}

			if new.Peer != "" {
				err := public.SetInterfaceSnatToSourceByNetwork(false, new.PhyifName, new.Peer, new.Address)
				if err != nil {
					agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", new.Id)
					return err, false
				}
			}
		}
	}

	if serverChg {
		/* reload configre and restart keeplived */
		/* 创建配置文件 */
		confFile, err := KeepalivedConfCreate(cfgNew)
		agentLog.AgentLogger.Info("Create file: ", confFile)
		if err != nil {
			agentLog.AgentLogger.Info("Create file failed ", confFile)
			return err, false
		}

		/* restart keeplived */
		if err = restartKeepalived(); err != nil {
			agentLog.AgentLogger.Info("restartKeepalived failed", err)
			return err, false
		}
	}

	return nil, chg
}

func (conf *HaConf) Destroy() error {

	/* 关闭HA服务 */
	var err error

	/* stop keepalived */
	if err = stopKeepalived(); err != nil {
		agentLog.AgentLogger.Info("stopKeepalived failed", err)
		return err
	}

	/* 删除配置文件 */
	confFile := keepalivedConfPath
	if public.FileExists(confFile) {
		agentLog.AgentLogger.Info("Remove file: ", confFile)
		err := os.Remove(confFile)
		if err != nil {
			agentLog.AgentLogger.Info("Remove file failed ", keepalivedConfPath)
			return err
		}
	}

	/* 删除iptables */
	for _, port := range conf.PortConfig {
		for _, network := range port.Networks {
			if port.Address != "" && network != "" && port.PhyifName != "" {
				err = public.SetInterfaceSnatToSourceByNetwork(true, port.PhyifName, network, port.Address)
				if err != nil {
					agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", port.Id)
					return err
				}
			}
		}

		if port.Peer != "" {
			err = public.SetInterfaceSnatToSourceByNetwork(true, port.PhyifName, port.Peer, port.Address)
			if err != nil {
				agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", port.Id)
				return err
			}
		}
	}

	return nil
}

func (fp *HaConf) Recover() error {

	/* 如果HA服务没有启动，则直接退出 */
	if !fp.Enable {
		return nil
	}

	/* 添加 iptables */
	for _, port := range fp.PortConfig {
		/* 设置iptables 规则 */
		for _, network := range port.Networks {
			if port.Address != "" && network != "" && port.PhyifName != "" {
				err := public.SetInterfaceSnatToSourceByNetwork(false, port.PhyifName, network, port.Address)
				if err != nil {
					agentLog.AgentLogger.Info("Ha SetInterfaceSnatToSourceByNetwork faild: ", port.Id)
					return err
				}
			}
		}
	}

	/* restart keeplived */
	if err := restartKeepalived(); err != nil {
		agentLog.AgentLogger.Info("restartKeepalived failed", err)
		return err
	}

	return nil
}

func GetHaWanInfo() (bool, string, string) {
	vipAddr := ""
	phyif := ""
	enable := false

	value, err := etcd.EtcdGetValue(config.HaConfPath)
	if err == nil {
		//Exist
		curConf := &HaConf{}
		if err = json.Unmarshal([]byte(value), curConf); err != nil {
			//errPanic(InternalError, InternalError, err)
			return enable, phyif, vipAddr
		}

		for _, port := range curConf.PortConfig {
			if port.Id == "wan1" {
				enable = curConf.Enable
				phyif = port.PhyifName
				vipAddr = port.VipAddr
				break
			}
		}
	}

	return enable, phyif, vipAddr
}
