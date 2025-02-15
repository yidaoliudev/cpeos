package app

import (
	"cpeos/agentLog"
	"cpeos/public"

	"gitlab.daho.tech/gdaho/util/derr"
)

const (
	/* Conn Type: Conn类型 */
	ConnType_Eport = 0
	ConnType_Gre   = 1
	ConnType_Ipsec = 2
	ConnType_Nat   = 3
	ConnType_Ssl   = 5

	/* Conn 路由类型 */
	ConnRouteType_Bgp    = 0
	ConnRouteType_Static = 1
)

type ConnConf struct {
	Id            string    `json:"id"`            //(必填)连接Id，使用全局唯一的Id序号
	PortId        string    `json:"portId"`        //(必填)PortId
	Type          int       `json:"type"`          //(必填)vlanif,gre,ipsec
	Bandwidth     int       `json:"bandwidth"`     //(必填)带宽限速，单位Mbps
	TunnelSrc     string    `json:"tunnelSrc"`     //(必填)本端网关地址，只有在HA场景填写VIP地址，否则填0.0.0.0/0
	TunnelDst     string    `json:"tunnelDst"`     //(必填)对端网关地址
	LocalAddress  string    `json:"localAddress"`  //(必填)
	RemoteAddress string    `json:"remoteAddress"` //(必填)
	HealthCheck   bool      `json:"healthCheck"`   //(必填)健康检查
	Nat           bool      `json:"nat"`           //(必填)SNAT开关
	NoNatCidr     []string  `json:"noNatCidr"`     //(必填)不NAT的cidr信息，默认为空
	IpsecInfo     IpsecConf `json:"ipsecInfo"`     //[选填]如果Type为ipsec
	SslInfo       SslConf   `json:"sslInfo"`       //[选填]如果Type为sslvpn
	ConnName      string    `json:"connName"`      //{不填}
}

func (conf *ConnConf) Create(action int) error {

	var err error

	/* set ConnName */
	conf.ConnName = conf.Id

	switch conf.Type {
	case ConnType_Eport:
		/* create vlan port */
	case ConnType_Gre:
		/* create gre tunnel */
	case ConnType_Ipsec:
		/* create ipsec tunnel */
		ipsec := &conf.IpsecInfo
		ipsec.Name = conf.ConnName
		if action == public.ACTION_ADD {
			if conf.TunnelSrc != "" {
				ipsec.TunnelSrc = conf.TunnelSrc
			} else {
				ipsec.TunnelSrc = "0.0.0.0/0"
			}
			ipsec.Bandwidth = 0
			ipsec.LifeTime = "3600"
			ipsec.TunnelDst = conf.TunnelDst
			ipsec.LocalAddress = conf.LocalAddress
			ipsec.RemoteAddress = conf.RemoteAddress
			ipsec.HealthCheck = conf.HealthCheck
			ipsec.Nat = conf.Nat
			ipsec.NoNatCidr = conf.NoNatCidr
		}
		err := ipsec.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn create ipsec fail: ", conf.ConnName)
			return err
		}
	case ConnType_Ssl:
		/* create ssl tunnel */
		ssl := &conf.SslInfo
		ssl.Name = conf.ConnName
		if action == public.ACTION_ADD {
			ssl.TunnelDst = conf.TunnelDst
			ssl.LocalAddress = conf.LocalAddress
			ssl.RemoteAddress = conf.RemoteAddress
			ssl.HealthCheck = conf.HealthCheck
			ssl.Nat = conf.Nat
			ssl.NoNatCidr = conf.NoNatCidr
		}
		err := ssl.Create(action)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn create ssl fail: ", conf.ConnName)
			return err
		}
	default:
		return derr.Error{In: err.Error(), Out: "ConnTypeError"}
	}

	/* 如果是recover，则直接返回 */
	if action == public.ACTION_RECOVER {
		return nil
	}

	return nil
}

func (cfgCur *ConnConf) Modify(cfgNew *ConnConf) (error, bool) {

	var chg = false
	var err error

	switch cfgCur.Type {
	case ConnType_Eport:
		/* Modify eport port */
	case ConnType_Gre:
		/* Modify gre tunnel */
	case ConnType_Ipsec:
		/* Modify ipsec tunnel */
		ipsec := &cfgCur.IpsecInfo
		ipsecNew := &cfgNew.IpsecInfo
		ipsecNew.Name = ipsec.Name
		ipsecNew.Bandwidth = 0
		ipsecNew.LifeTime = "3600"
		if cfgNew.TunnelSrc != "" {
			ipsecNew.TunnelSrc = cfgNew.TunnelSrc
		} else {
			ipsecNew.TunnelSrc = "0.0.0.0/0"
		}
		ipsecNew.TunnelDst = cfgNew.TunnelDst
		ipsecNew.LocalAddress = cfgNew.LocalAddress
		ipsecNew.RemoteAddress = cfgNew.RemoteAddress
		ipsecNew.HealthCheck = cfgNew.HealthCheck
		ipsecNew.Nat = cfgNew.Nat
		ipsecNew.NoNatCidr = cfgNew.NoNatCidr
		err, chg = ipsec.Modify(ipsecNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn Modify ipsec fail: ", cfgCur.ConnName)
			return err, false
		}
	case ConnType_Ssl:
		/* Modify ssl tunnel */
		ssl := &cfgCur.SslInfo
		sslNew := &cfgNew.SslInfo
		sslNew.Name = ssl.Name
		sslNew.Bandwidth = 0
		sslNew.TunnelDst = cfgNew.TunnelDst
		sslNew.LocalAddress = cfgNew.LocalAddress
		sslNew.RemoteAddress = cfgNew.RemoteAddress
		sslNew.HealthCheck = cfgNew.HealthCheck
		sslNew.Nat = cfgNew.Nat
		sslNew.NoNatCidr = cfgNew.NoNatCidr
		err, chg = ssl.Modify(sslNew)
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn Modify ssl fail: ", cfgCur.ConnName)
			return err, false
		}
	default:
		return derr.Error{In: err.Error(), Out: "ConnTypeError"}, false
	}

	return nil, chg
}

func (conf *ConnConf) Destroy() error {

	switch conf.Type {
	case ConnType_Eport:
		/* Destroy vlan port */
	case ConnType_Gre:
		/* Destroy gre port */
	case ConnType_Ipsec:
		/* Destroy ipsec port */
		ipsec := &conf.IpsecInfo
		err := ipsec.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn destroy ipsec fail: ", conf.ConnName)
			return err
		}
	case ConnType_Ssl:
		/* Destroy ssl port */
		ssl := &conf.SslInfo
		err := ssl.Destroy()
		if err != nil {
			agentLog.AgentLogger.Info(err, "Conn destroy ssl fail: ", conf.ConnName)
			return err
		}
	default:
	}

	return nil
}
