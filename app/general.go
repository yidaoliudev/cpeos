package app

import (
	"cpeos/agentLog"
	"cpeos/config"
	"cpeos/etcd"
	"cpeos/public"
	"encoding/json"
	"io/ioutil"
	"strings"
)

const (
	chinaRoutePath    = "/home/chinaRoute.conf"
	chinaRouteExcPath = "/home/chinaRouteExc.conf"
	chinaRouteTable   = 100          /* ip route表ID */
	chinaRoutePref    = 100          /* ip rule优先级 */
	chinaRouteMask    = "100"        /* iptables标签 */
	chinaRouteObj     = "chinaroute" /* ipset对象实例名称 */
)

type GeneralConf struct {
	LocalNat          bool     `json:"localNat"`
	LocalNatHa        bool     `json:"localNatHa"`
	SdwanNat          bool     `json:"sdwanNat"`
	SdwanStaticRelate bool     `json:"sdwanStaticRelate"`
	ChinaRoute        bool     `json:"chinaRoute"`
	ChinaRtDevice     string   `json:"chinaRtDevice"`
	ChinaRtExtStatic  []string `json:"chinaRtExtStatic"`
}

func LoadChinaRouteInfo(action int, conf *GeneralConf) error {

	data, err := ioutil.ReadFile(chinaRoutePath) //read the file
	if err != nil {
		agentLog.AgentLogger.Info("general not found: ", chinaRoutePath)
		return nil
	}

	route_lines := strings.Split(string(data), "\n")
	for _, member := range route_lines {
		if member == "" {
			// 如果内容是空
			continue
		}
		err = public.IpsetMemberSet(false, chinaRouteObj, member, false)
		if err != nil {
			return err
		}
	}

	//Exclude Wan IP address
	_, addr := GetPortNexthopById("wan1")
	if addr != "" {
		member := strings.Split(addr, "/")[0] + "/32"
		err = public.IpsetMemberSet(false, chinaRouteObj, member, true)
		if err != nil {
			return err
		}
	}

	if public.FileExists(chinaRouteExcPath) {
		data, err := ioutil.ReadFile(chinaRouteExcPath) //read the file
		if err != nil {
			agentLog.AgentLogger.Info("general not found: ", chinaRouteExcPath)
			return nil
		}

		route_lines := strings.Split(string(data), "\n")
		for _, member := range route_lines {
			if member == "" {
				// 如果内容是空
				continue
			}
			err = public.IpsetMemberSet(false, chinaRouteObj, member, true)
			if err != nil {
				return err
			}
		}
	}

	//Excude Static Route
	if action == public.ACTION_ADD {
		// read form static StaticConfPath
		conf.ChinaRtExtStatic = GetStaticExtMembes()
	}

	// recovery
	for _, member := range conf.ChinaRtExtStatic {
		err = public.IpsetMemberSet(false, chinaRouteObj, member, true)
		if err != nil {
			return err
		}
	}

	return nil
}

func CreateChinaRoutePolicy(action int, nexthop string, conf *GeneralConf) error {

	var err error

	/* 新建 */
	err = public.IpsetObjCreate(chinaRouteObj, "hash:net", 1000000)
	if err != nil {
		agentLog.AgentLogger.Info("IpsetObjCreate error")
		return err
	}

	err = LoadChinaRouteInfo(action, conf)
	if err != nil {
		agentLog.AgentLogger.Info("LoadChinaRouteInfo error")
		return err
	}

	err = public.SetIpRuleWithMark(false, chinaRouteMask, chinaRouteTable, chinaRoutePref)
	if err != nil {
		agentLog.AgentLogger.Info("SetIpRuleWithMark error")
		return err
	}

	if action == public.ACTION_ADD {
		err, _ = IpRouteWithTable(false, "0.0.0.0/0", nexthop, chinaRouteTable)
		if err != nil {
			agentLog.AgentLogger.Info("IpRouteWithTable error")
			return err
		}
	}

	err = public.SetIptablesMask(false, chinaRouteObj, chinaRouteMask)
	if err != nil {
		agentLog.AgentLogger.Info("SetIptablesMask error")
		return err
	}

	return nil
}

func DestroyChinaRoutePolicy(nexthop string) error {
	var err error

	/* 删除 */
	err = public.SetIptablesMask(true, chinaRouteObj, chinaRouteMask)
	if err != nil {
		agentLog.AgentLogger.Info("SetIptablesMask error")
		return err
	}

	err = public.SetIpRuleWithMark(true, chinaRouteMask, chinaRouteTable, chinaRoutePref)
	if err != nil {
		agentLog.AgentLogger.Info("SetIpRuleWithMark error")
		return err
	}

	err, _ = IpRouteWithTable(true, "0.0.0.0/0", nexthop, chinaRouteTable)
	if err != nil {
		agentLog.AgentLogger.Info("IpRouteWithTable error")
		return err
	}

	err = public.IpsetObjDestroy(chinaRouteObj)
	if err != nil {
		agentLog.AgentLogger.Info("IpsetObjDestroy error")
		return err
	}

	return nil
}

func (conf *GeneralConf) Create(action int) error {

	if conf.LocalNat {
		//设置wan接口的snat策略，只考虑非HA模式
		err, port := GetPortInfoById("wan1")
		if err != nil {
			agentLog.AgentLogger.Info("general localNat not found wan1.")
		} else {
			public.SetInterfaceSnat(false, port.PhyifName)
		}
	}

	if conf.ChinaRoute {
		//设置所有中国网段静态路由，指向wan接口的下一跳，如果没有下一跳，则不下发
		var nexthop string
		if conf.ChinaRtDevice == "" || conf.ChinaRtDevice == "wan1" {
			nexthop, _ = GetPortNexthopById("wan1")
		} else {
			nexthop = conf.ChinaRtDevice
		}
		if nexthop != "" {
			/* 新建 */
			CreateChinaRoutePolicy(action, nexthop, conf)
		}
	}

	return nil
}

func (cfgCur *GeneralConf) Modify(cfgNew *GeneralConf) (error, bool) {

	chg := false
	if cfgCur.LocalNat != cfgNew.LocalNat {
		//设置wan接口的snat策略，只考虑非HA模式
		err, port := GetPortInfoById("wan1")
		if err != nil {
			agentLog.AgentLogger.Info("general localNat not found wan1.")
		}

		if cfgCur.LocalNat {
			public.SetInterfaceSnat(true, port.PhyifName)
		} else {
			public.SetInterfaceSnat(false, port.PhyifName)
		}
		chg = true
	}

	if cfgCur.LocalNatHa != cfgNew.LocalNatHa {
		chg = true
	}

	if cfgCur.SdwanNat != cfgNew.SdwanNat {
		chg = true
	}

	if cfgCur.SdwanStaticRelate != cfgNew.SdwanStaticRelate {
		chg = true
	}

	if cfgCur.ChinaRoute != cfgNew.ChinaRoute {
		if cfgCur.ChinaRoute {
			/* 关闭 */
			var nexthop string
			if cfgCur.ChinaRtDevice == "" || cfgCur.ChinaRtDevice == "wan1" {
				nexthop, _ = GetPortNexthopById("wan1")
			} else {
				nexthop = cfgCur.ChinaRtDevice
			}
			if nexthop != "" {
				DestroyChinaRoutePolicy(nexthop)
			}
		} else {
			/* 新建 */
			var nexthop string
			if cfgNew.ChinaRtDevice == "" || cfgCur.ChinaRtDevice == "wan1" {
				nexthop, _ = GetPortNexthopById("wan1")
			} else {
				nexthop = cfgNew.ChinaRtDevice
			}
			if nexthop != "" {
				CreateChinaRoutePolicy(public.ACTION_ADD, nexthop, cfgNew)
			}
		}
		chg = true
	} else if cfgCur.ChinaRoute {
		if cfgCur.ChinaRtDevice != cfgNew.ChinaRtDevice {
			/* chinaRoute开启时,修改nexthop */
			var nexthopCur string
			if cfgCur.ChinaRtDevice == "" || cfgCur.ChinaRtDevice == "wan1" {
				nexthopCur, _ = GetPortNexthopById("wan1")
			} else {
				nexthopCur = cfgCur.ChinaRtDevice
			}
			if nexthopCur != "" {
				IpRouteWithTable(true, "0.0.0.0/0", nexthopCur, chinaRouteTable)
			}

			var nexthopNew string
			if cfgNew.ChinaRtDevice == "" || cfgNew.ChinaRtDevice == "wan1" {
				nexthopNew, _ = GetPortNexthopById("wan1")
			} else {
				nexthopNew = cfgNew.ChinaRtDevice
			}
			if nexthopNew != "" {
				IpRouteWithTable(false, "0.0.0.0/0", nexthopNew, chinaRouteTable)
			}
			chg = true
		}

		/* 只要开启chinaRoute，每次都要获取最新的member*/
		cfgNew.ChinaRtExtStatic = GetStaticExtMembes()
		add, delete := public.Arrcmp(cfgCur.ChinaRtExtStatic, cfgNew.ChinaRtExtStatic)
		if len(delete) != 0 || len(add) != 0 {
			if len(delete) != 0 {
				for _, member := range delete {
					public.IpsetMemberSet(true, chinaRouteObj, member, true)
				}
			}
			if len(add) != 0 {
				for _, member := range add {
					public.IpsetMemberSet(false, chinaRouteObj, member, true)
				}
			}
			chg = true
		}
	}

	return nil, chg
}

func UpdateChinaRoute(port *PortConf, newNexthop, newIpAddr string) error {

	value, err := etcd.EtcdGetValue(config.GeneralConfPath)
	if err != nil {
		agentLog.AgentLogger.Info("general not found: ", err.Error())
		return nil
	}

	bytes := []byte(value)
	fp := &GeneralConf{}
	err = json.Unmarshal(bytes, fp)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]general data unmarshal failed: ", err.Error())
		return nil
	}

	device := "wan1"
	if fp.ChinaRtDevice != "" {
		device = fp.ChinaRtDevice
	}

	if fp.ChinaRoute && device == "wan1" {
		if port.Nexthop == "" {
			/* 新建 */
			CreateChinaRoutePolicy(public.ACTION_ADD, newNexthop, fp)
		} else {
			/* 修改下一跳 */
			IpRouteReplaceWithTable("0.0.0.0/0", port.Nexthop, newNexthop, chinaRouteTable)
			if port.IpAddr != newIpAddr {
				//
				if port.IpAddr != "" {
					member := strings.Split(port.IpAddr, "/")[0] + "/32"
					err = public.IpsetMemberSet(true, chinaRouteObj, member, true)
					if err != nil {
						return err
					}
				}

				if newIpAddr != "" {
					member := strings.Split(newIpAddr, "/")[0] + "/32"
					err = public.IpsetMemberSet(false, chinaRouteObj, member, true)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func GetLocalNatHa() bool {

	enable := false
	value, err := etcd.EtcdGetValue(config.GeneralConfPath)
	if err == nil {
		//Exist
		curConf := &GeneralConf{}
		if err = json.Unmarshal([]byte(value), curConf); err != nil {
			//errPanic(InternalError, InternalError, err)
			return enable
		}

		enable = curConf.LocalNatHa
	}

	return enable
}
