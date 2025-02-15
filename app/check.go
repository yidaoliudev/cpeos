package app

import (
	"strings"
)

/**
概述：Check用于定义CPE上的网络检测任务。

检测类型有：
1, ping
2, tcping [暂不支持]

**/

type CheckConf struct {
	Id      string `json:"id"`      //(必填)Id
	Target  string `json:"target"`  //目标IP
	Device  string `json:"device"`  //(必填)检查出口
	DevAddr string `json:"devAddr"` //{不填}记录出口IP地址信息
	Nexthop string `json:"nexthop"` //{不填}记录出口名称或下一跳信息
}

func (conf *CheckConf) Create() error {

	nexthop, devAddr := GetPortNexthopById(strings.ToLower(conf.Device))
	conf.Nexthop = nexthop
	conf.DevAddr = devAddr
	return nil
}

func (cfgCur *CheckConf) Modify(cfgNew *CheckConf) (error, bool) {

	return nil, false
}

func (conf *CheckConf) Destroy() error {
	return nil
}
