package app

import (
	"cpeos/agentLog"
	"cpeos/config"
	"cpeos/etcd"
	"cpeos/public"
	"encoding/json"
	"strings"
)

type SubnetConf struct {
	Id      string   `json:"id"`      //(必填)物理接口的名称，例如：wan1，lan1等  //不可修改
	Cidrs   []string `json:"cidrs"`   //子网网段信息，可修改
	DevAddr string   `json:"devAddr"` //{不填}记录出口IP地址信息
	Nexthop string   `json:"nexthop"` //{不填}记录出口名称或下一跳信息
}

func (conf *SubnetConf) Create() error {

	/* 只有lan和wan的subnet配置路由 */
	if !strings.Contains(strings.ToLower(conf.Id), strings.ToLower("wan")) &&
		!strings.Contains(strings.ToLower(conf.Id), strings.ToLower("lan")) {
		return nil
	}

	nexthop, devAddr := GetPortNexthopById(strings.ToLower(conf.Id))
	if nexthop == "" {
		/* 如果没有下一跳则不下发路由 */
		return nil
	}

	err := IpRouteBatch(false, conf.Cidrs, nexthop)
	if err != nil {
		agentLog.AgentLogger.Info("AddRoute error")
		return err
	}

	conf.Nexthop = nexthop
	conf.DevAddr = devAddr
	return nil
}

func (cfgCur *SubnetConf) Modify(cfgNew *SubnetConf) (error, bool) {

	add, delete := public.Arrcmp(cfgCur.Cidrs, cfgNew.Cidrs)
	if len(add) == 0 && len(delete) == 0 {
		return nil, false
	}

	/* 只有lan和wan的subnet配置路由 */
	if !strings.Contains(strings.ToLower(cfgCur.Id), strings.ToLower("wan")) &&
		!strings.Contains(strings.ToLower(cfgCur.Id), strings.ToLower("lan")) {
		cfgCur.Cidrs = cfgNew.Cidrs
		return nil, true
	}

	if cfgCur.Nexthop == "" {
		cfgCur.Cidrs = cfgNew.Cidrs
		return nil, true
	}

	if len(add) != 0 {
		/* add */
		err := IpRouteBatch(false, add, cfgCur.Nexthop)
		if err != nil {
			agentLog.AgentLogger.Info("AddRoute error")
			return err, false
		}
	}
	if len(delete) != 0 {
		/* del */
		err := IpRouteBatch(true, delete, cfgCur.Nexthop)
		if err != nil {
			agentLog.AgentLogger.Info("DelRoute error")
			return err, false
		}
	}

	cfgCur.Cidrs = cfgNew.Cidrs
	return nil, true
}

func (conf *SubnetConf) Destroy() error {

	/* 只有lan和wan的subnet配置路由 */
	if !strings.Contains(strings.ToLower(conf.Id), strings.ToLower("wan")) &&
		!strings.Contains(strings.ToLower(conf.Id), strings.ToLower("lan")) {
		return nil
	}

	if conf.Nexthop == "" {
		return nil
	}

	err := IpRouteBatch(true, conf.Cidrs, conf.Nexthop)
	if err != nil {
		agentLog.AgentLogger.Info("DelRoute error")
		return err
	}

	return nil
}

func UpdateSubnetNexhop(port *PortConf, newNexthop string) error {
	paths := []string{config.SubnetConfPath}
	subnets, err := etcd.EtcdGetValues(paths)
	if err == nil {
		for _, value := range subnets {
			bytes := []byte(value)
			fp := &SubnetConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}
			if fp.Id != port.Id {
				continue
			}

			if fp.Nexthop == newNexthop {
				continue
			}

			/* 遍历subnet的cidr，修改nexthop */
			if fp.Nexthop == "" {
				IpRouteBatch(false, fp.Cidrs, newNexthop)
			} else if newNexthop == "" {
				IpRouteBatch(true, fp.Cidrs, fp.Nexthop)
			} else {
				IpRouteBatchReplace(fp.Cidrs, fp.Nexthop, newNexthop)
			}

			/* 修改 etcd 数据 */
			fp.Nexthop = newNexthop
			fp.DevAddr = port.IpAddr
			saveData, err := json.Marshal(fp)
			if err != nil {
				agentLog.AgentLogger.Info("Marshal error " + config.SubnetConfPath + fp.Id)
				continue
			}

			agentLog.AgentLogger.Info("Subnet etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.SubnetConfPath+fp.Id, string(saveData[:]))
			if err != nil {
				agentLog.AgentLogger.Info("etcd save error " + config.SubnetConfPath + fp.Id)
			}
		}
	}

	return nil
}

func GetSubnetNetworks() []string {
	var networks []string
	var cidrs []string

	paths := []string{config.SubnetConfPath}
	subs, err := etcd.EtcdGetValues(paths)
	if err == nil {
		for _, value := range subs {
			bytes := []byte(value)
			fp := &SubnetConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			if strings.Contains(strings.ToLower(fp.Id), strings.ToLower("wan")) ||
				strings.Contains(strings.ToLower(fp.Id), strings.ToLower("lan")) {
				for _, cidr := range fp.Cidrs {
					cidrs = append(cidrs, cidr)
				}
			} else {
				/* sdwan zone */
				for _, cidr := range fp.Cidrs {
					networks = append(networks, cidr)
				}
			}
		}
	}

	if len(networks) == 0 {
		/* 如果没有sdwan zone的子网，则使用lan、wan的子网配置 */
		return public.SliceRemoveDuplicates(cidrs)
	}

	return public.SliceRemoveDuplicates(networks)
}
