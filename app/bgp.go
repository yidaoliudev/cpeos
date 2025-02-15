package app

import (
	"context"
	"cpeos/agentLog"
	"cpeos/config"
	mceetcd "cpeos/etcd"
	"cpeos/public"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

var (
	bgpMonitorInterval int
	BgpStatusURL       = "/api/cpeConfig/bgps/status"
)

const (
	maxRcvPrefix               = 1000
	cmd_vtysh                  = "/bin/vtysh"
	cmd_vtysh_op_c             = "-c"
	cmd_reverse                = "no"
	BgpAttribute_Med           = "Med"
	BgpAttribute_As_Path       = "AsPaths"
	RouteMapActionPermit       = "permit"
	RouteMapActionDeny         = "deny"
	routemap_type_root         = "root"
	routemap_type_default      = "default"
	routemap_type_user         = "user"
	bgpPolicyDirectOut         = "out"
	bgpPolicyDirectIn          = "in"
	cmd_GotoVrf                = "vrf %s"
	cmd_SetIpNht               = "ip nht resolve-via-default"
	cmd_showVrfNeigh           = "show ip bgp vrf %s neighbors %s json"
	cmd_showVrfRcvRoutes       = "show ip bgp vrf %s neighbors %s routes json"
	cmd_showroute              = "show ip bgp json"
	cmd_showBgpSumm            = "show ip bgp summary json"
	cmd_showRoute              = "show ip route json"
	cmd_showVrfRoute           = "show ip route vrf %s json"
	cmd_configTerm             = "configure terminal"
	cmd_familyipv4             = "address-family ipv4 unicast"
	cmd_routercreate           = "router bgp %d"
	cmd_routerVrfCreate        = "router bgp %v vrf %s"
	cmd_routerid               = "bgp router-id %s"
	cmd_clusterid              = "bgp cluster-id %s"
	cmd_neighadd               = "neighbor %s remote-as %d"
	cmd_neighdes               = "neighbor %s description %s"
	cmd_neighPass              = "neighbor %s password %s"
	cmd_neighHoldTimeKeepAlive = "neighbor %s timers %d %d"
	cmd_neighreflector         = "neighbor %s route-reflector-client"
	cmd_dowrite                = "do write"
	cmd_neighMaxPrefix         = "neighbor %s maximum-prefix %d"
	cmd_networkPrefix          = "network %s"
	cmd_neighRouteMap          = "neighbor %s route-map %s %s"
	cmd_ebgpMultihop           = "neighbor %s ebgp-multihop %d" // config ebgp-multihop to 255
	cmd_redistributeKennel     = "redistribute kernel"
	cmd_RouteMap               = "route-map %s %s %d"
	cmd_callRouteMap           = "call %s"
	cmd_OnMathNext             = "on-match next"
	cmd_PrependAsPath          = "set as-path prepend"
	cmd_setMetric              = "set metric %s"
	cmd_setMetricNone          = "no set metric"
	cmd_vrfIpRouteAdd          = "ip route %s %s vrf %s"
	cmd_IpRouteAdd             = "ip route %s %s"
	cmd_IpRouteWithTableAdd    = "ip route %s %s table %d"
	cmd_EbgpRequirePolicy      = "bgp ebgp-requires-policy"
	cmd_SuppressDuplicates     = "bgp suppress-duplicates"
	cmd_NetworkImportCheck     = "bgp network import-check"
	cmd_NeighborRouteMapOut    = "neighbor %s route-map %s out"
	cmd_NeighborBfd            = "neighbor %s bfd profile %s"
	cmd_NeighClear             = "clear ip bgp  %s"
)

/*
BGP路由配置
*/

type NeighConf struct {
	Id          string `json:"id"`          //(必填)Id全局唯一
	PeerAddress string `json:"peerAddress"` //(必填)
	PeerAs      uint64 `json:"peerAs"`      //(必填)
	EbgpMutihop int    `json:"ebgpMutihop"` //[选填]
	KeepAlive   int    `json:"keepAlive"`   //[选填]
	HoldTime    int    `json:"holdTime"`    //[选填]
	MaxPerfix   int    `json:"maxPerfix"`   //[选填]
	Password    string `json:"password"`    //[选填]
}

type BgpConf struct {
	LocalAs     uint64      `json:"localAs"`     //(必填)BGP本端AS
	RouterId    string      `json:"routerId"`    //[选填]
	NeighConfig []NeighConf `json:"neighConfig"` //(必填)BGP邻居
	Networks    []string    `json:"Networks"`    //{不填}BGP宣告子网信息
}

type neighborStatus struct {
	peer    *peerInfo
	routes  *routesInfo
	ageFlag bool
}

type peerInfo struct {
	bgpConfId string
	remoteAs  uint64
	remoteIp  string
	BgpState  string `json:"bgpState"`
	chgFlag   bool
}

type routesInfo struct {
	VrfName string                   `json:"vrfName"`
	Routes  map[string][]routeEffect `json:"routes"`
	chgFlag bool
}

type reportBgpInfo struct {
	BgpConfId       string       `json:"id"`
	Status          string       `json:"status"`
	BgpRouteDetails []routerinfo `json:"bgpRouteDetails"`
}

type routerinfo struct {
	Network string `json:"destinationNetwork"`
	Status  string `json:"status"`
	NextHop string `json:"nextHop"`
	AsPath  string `json:"asPath"`
}

type routeEffect struct {
	Valid    bool           `json:"valid"`
	Bestpath bool           `json:"bestpath"`
	Network  string         `json:"network"`
	Aspath   string         `json:"aspath"`
	Nexthops []nexthopsInfo `json:"nexthops"`
}

type nexthopsInfo struct {
	Ip string `json:"ip"`
}

func cmdrNeighClear(neigh string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	elems := strings.Split(fmt.Sprintf(cmd_NeighClear, neigh), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdroutercreate(undo bool, as uint64) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}

	elems := strings.Split(fmt.Sprintf(cmd_routercreate, as), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdEbgpRequirePolicy(undo bool) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}

	elems := strings.Split(fmt.Sprintf(cmd_EbgpRequirePolicy), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}
func cmdSuppressDuplicates(undo bool) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}

	elems := strings.Split(fmt.Sprintf(cmd_SuppressDuplicates), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}
func cmdNetworkImportCheck(undo bool) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}

	elems := strings.Split(fmt.Sprintf(cmd_NetworkImportCheck), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdneighNetworkRoutes(undo bool, prefix string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_networkPrefix, prefix), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdfamilyipv4() []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	elems := strings.Split(cmd_familyipv4, " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdneighRouteMapOut(undo bool, neighbor string, routeMap string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_NeighborRouteMapOut, neighbor, routeMap), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdneighBfd(undo bool, neighbor string, routeMap string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}

	elems := strings.Split(fmt.Sprintf(cmd_NeighborBfd, neighbor, routeMap), " ")

	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdneighadd(undo bool, ip string, as uint64) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_neighadd, ip, as), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdRouterid(undo bool, routerid string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_routerid, routerid), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdEbgpMultiHop(undo bool, ip string, ebgpMultihop int) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_ebgpMultihop, ip, ebgpMultihop), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdneighPasswd(undo bool, ip string, passwd string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_neighPass, ip, passwd), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdneighKeepAliveHoldTime(ip string, keepAlive int, holdTime int) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	elems := strings.Split(fmt.Sprintf(cmd_neighHoldTimeKeepAlive, ip, keepAlive, holdTime), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd

}

func cmdneighMaxPrefix(undo bool, ip string, maxPrefix int) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_neighMaxPrefix, ip, maxPrefix), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdconfigTerm() []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	elems := strings.Split(cmd_configTerm, " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmddowrite() []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	elems := strings.Split(cmd_dowrite, " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func CmdShowBgpNeigh(vrf string, neigh string) []string {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmd_vtysh_op_c)
	cmd = append(cmd, "\"")

	if vrf == "" {
		cmd = append(cmd, cmd_showBgpSumm)
	} else {
		cmd = append(cmd, fmt.Sprintf(cmd_showVrfNeigh, vrf, neigh))
	}

	cmd = append(cmd, "\"")
	return cmd
}

func CmdShowRoute(vrf string) []string {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmd_vtysh_op_c)
	cmd = append(cmd, "\"")

	if vrf == "default" {
		cmd = append(cmd, cmd_showRoute)
	} else {
		cmd = append(cmd, fmt.Sprintf(cmd_showVrfRoute, vrf))
	}

	cmd = append(cmd, "\"")
	return cmd
}

func cmdShowBgpRoutes(vrf string, neigh string) []string {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmd_vtysh_op_c)
	cmd = append(cmd, "\"")

	if vrf == "" {
		cmd = append(cmd, cmd_showroute)
	} else {
		cmd = append(cmd, fmt.Sprintf(cmd_showVrfRcvRoutes, vrf, neigh))
	}

	cmd = append(cmd, "\"")
	return cmd
}

func cmdIpRoute(undo bool, prefix string, device string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_IpRouteAdd, prefix, device), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdIpRouteWithTable(undo bool, prefix string, device string, tableId int) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_IpRouteWithTableAdd, prefix, device, tableId), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func cmdVrfIpRoute(undo bool, prefix string, device string, vrf string) []string {
	cmd := []string{cmd_vtysh_op_c, "\""}
	if undo {
		cmd = append(cmd, "no")
	}
	elems := strings.Split(fmt.Sprintf(cmd_vrfIpRouteAdd, prefix, device, vrf), " ")
	cmd = append(cmd, elems...)
	cmd = append(cmd, "\"")
	return cmd
}

func BgpAsVrfConf(undo bool, loalAs uint64, namespaceId string) error {

	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdroutercreate(undo, loalAs)...)
	cmd = append(cmd, cmddowrite()...)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	agentLog.AgentLogger.Info("BgpAsVrfConf cmd: ", cmd, err)
	if err != nil {
		return err
	}
	return nil
}

func cmdConfigBgpNeighAll(conf *BgpConf, undo bool) []string {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdroutercreate(false, conf.LocalAs)...)
	cmd = append(cmd, cmdEbgpRequirePolicy(true)...)
	cmd = append(cmd, cmdSuppressDuplicates(true)...)
	cmd = append(cmd, cmdNetworkImportCheck(true)...)
	if conf.RouterId != "" {
		cmd = append(cmd, cmdRouterid(undo, conf.RouterId)...)
	}

	for _, neigh := range conf.NeighConfig {
		cmd = append(cmd, cmdneighadd(undo, neigh.PeerAddress, neigh.PeerAs)...)
		cmd = append(cmd, cmdneighMaxPrefix(undo, neigh.PeerAddress, maxRcvPrefix)...)
		if neigh.EbgpMutihop != 0 {
			cmd = append(cmd, cmdEbgpMultiHop(undo, neigh.PeerAddress, neigh.EbgpMutihop)...)
		} else {
			cmd = append(cmd, cmdEbgpMultiHop(undo, neigh.PeerAddress, 255)...)
		}
		if neigh.Password != "" {
			cmd = append(cmd, cmdneighPasswd(undo, neigh.PeerAddress, neigh.Password)...)
		}
		if neigh.KeepAlive != 0 && neigh.HoldTime != 0 {
			cmd = append(cmd, cmdneighKeepAliveHoldTime(neigh.PeerAddress, neigh.KeepAlive, neigh.HoldTime)...)
		}
	}
	cmd = append(cmd, cmdfamilyipv4()...)

	for _, neigh := range conf.NeighConfig {
		if neigh.MaxPerfix != 0 {
			cmd = append(cmd, cmdneighMaxPrefix(undo, neigh.PeerAddress, neigh.MaxPerfix)...)
		}
	}

	for _, cidr := range conf.Networks {
		cmd = append(cmd, cmdneighNetworkRoutes(undo, cidr)...)
	}

	cmd = append(cmd, cmddowrite()...)
	return cmd
}

func cmdModfiyBgpNeigh(curConf *BgpConf, newConf *BgpConf) []string {
	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdroutercreate(false, curConf.LocalAs)...)
	cmd = append(cmd, cmdEbgpRequirePolicy(true)...)
	cmd = append(cmd, cmdSuppressDuplicates(true)...)
	cmd = append(cmd, cmdNetworkImportCheck(true)...)

	/* 比较router id */
	if curConf.RouterId != newConf.RouterId {
		if curConf.RouterId != "" {
			cmd = append(cmd, cmdRouterid(true, curConf.RouterId)...)
		}
		if newConf.RouterId != "" {
			cmd = append(cmd, cmdRouterid(false, newConf.RouterId)...)
		}
	}

	/* 比较neighbor配置 */
	found := false
	for _, old := range curConf.NeighConfig {
		found = false
		for _, new := range newConf.NeighConfig {
			if old.Id == new.Id {
				/* 如果PeerAddress改变 */
				if old.PeerAddress != new.PeerAddress {
					cmd = append(cmd, cmdneighadd(true, old.PeerAddress, old.PeerAs)...)
					cmd = append(cmd, cmdneighadd(false, new.PeerAddress, new.PeerAs)...)
					cmd = append(cmd, cmdneighMaxPrefix(false, new.PeerAddress, maxRcvPrefix)...)
					if new.EbgpMutihop != 0 {
						cmd = append(cmd, cmdEbgpMultiHop(false, new.PeerAddress, new.EbgpMutihop)...)
					} else {
						cmd = append(cmd, cmdEbgpMultiHop(false, new.PeerAddress, 255)...)
					}
					if new.Password != "" {
						cmd = append(cmd, cmdneighPasswd(false, new.PeerAddress, new.Password)...)
					}
					if new.KeepAlive != 0 && new.HoldTime != 0 {
						cmd = append(cmd, cmdneighKeepAliveHoldTime(new.PeerAddress, new.KeepAlive, new.HoldTime)...)
					}
				} else {
					if old.PeerAs != new.PeerAs {
						cmd = append(cmd, cmdneighadd(false, new.PeerAddress, new.PeerAs)...)
					}
					if old.EbgpMutihop != new.EbgpMutihop {
						cmd = append(cmd, cmdEbgpMultiHop(false, new.PeerAddress, new.EbgpMutihop)...)
					}
					if old.Password != new.Password {
						if old.Password != "" {
							cmd = append(cmd, cmdneighPasswd(true, old.PeerAddress, old.Password)...)
						}
						if new.Password != "" {
							cmd = append(cmd, cmdneighPasswd(false, new.PeerAddress, new.Password)...)
						}
					}

					if old.KeepAlive != new.KeepAlive || old.HoldTime != new.HoldTime {
						cmd = append(cmd, cmdneighKeepAliveHoldTime(new.PeerAddress, new.KeepAlive, new.HoldTime)...)
					}
				}
				found = true
				break
			}
		}

		if !found {
			//delete old neighbor
			cmd = append(cmd, cmdneighadd(true, old.PeerAddress, old.PeerAs)...)
		}
	}

	for _, new := range newConf.NeighConfig {
		found = false
		for _, old := range curConf.NeighConfig {
			if old.Id == new.Id {
				found = true
				break
			}
		}

		if !found {
			//add new neighbor
			cmd = append(cmd, cmdneighadd(false, new.PeerAddress, new.PeerAs)...)
			cmd = append(cmd, cmdneighMaxPrefix(false, new.PeerAddress, maxRcvPrefix)...)
			if new.EbgpMutihop != 0 {
				cmd = append(cmd, cmdEbgpMultiHop(false, new.PeerAddress, new.EbgpMutihop)...)
			} else {
				cmd = append(cmd, cmdEbgpMultiHop(false, new.PeerAddress, 255)...)
			}
			if new.Password != "" {
				cmd = append(cmd, cmdneighPasswd(false, new.PeerAddress, new.Password)...)
			}
			if new.KeepAlive != 0 && new.HoldTime != 0 {
				cmd = append(cmd, cmdneighKeepAliveHoldTime(new.PeerAddress, new.KeepAlive, new.HoldTime)...)
			}
		}
	}

	cmd = append(cmd, cmdfamilyipv4()...)

	/* network */
	add, delete := public.Arrcmp(curConf.Networks, newConf.Networks)
	if len(delete) != 0 {
		for _, cidr := range delete {
			cmd = append(cmd, cmdneighNetworkRoutes(true, cidr)...)
		}
	}
	if len(add) != 0 {
		for _, cidr := range add {
			cmd = append(cmd, cmdneighNetworkRoutes(false, cidr)...)
		}
	}

	cmd = append(cmd, cmddowrite()...)
	return cmd
}
func Bgpdel(conf *BgpConf) []string {

	cmd := []string{cmd_vtysh}
	cmd = append(cmd, cmdconfigTerm()...)
	cmd = append(cmd, cmdroutercreate(true, conf.LocalAs)...)
	cmd = append(cmd, cmddowrite()...)

	return cmd
}

func BgpClear(conf *BgpConf) []string {

	cmd := []string{cmd_vtysh}
	//TODO
	///cmd = append(cmd, cmdrNeighClear(conf.PeerAddress)...)

	return cmd
}

/***
** Update bgp status to core.
** Start.
****/

func neighsStatusAgeStart(neighsStatus map[string]*neighborStatus) {
	for _, neighS := range neighsStatus {
		neighS.ageFlag = true
	}
}

func neighsStatusAgeEnd(neighsStatus map[string]*neighborStatus) {
	// 不考虑邻居的老化上报，配置层面邻居被删除认为是控制器删除的
	for bgpConfId, neighS := range neighsStatus {
		if neighS.ageFlag {
			delete(neighsStatus, bgpConfId)
		}
	}
}

func neighsStatusUpdate(neigh *NeighConf, neighsStatus map[string]*neighborStatus, neighout string, routeout string) error {
	// 去除不需要的信息，上报最小单位信息
	neighStatus := make(map[string]peerInfo)
	if err := json.Unmarshal([]byte(neighout), &neighStatus); err != nil {
		return fmt.Errorf("unmarshal neighout %s to neighStatus %v err: %v", neighout, neighStatus, err)
	}

	routes := &routesInfo{}
	if err := json.Unmarshal([]byte(routeout), routes); err != nil {
		return fmt.Errorf("unmarshal routeout %s to routes %v err: %v", routeout, routes, err)
	}

	// 刷新当前状态信息
	for peerIp, peer := range neighStatus {

		peer.remoteIp = peerIp
		peer.bgpConfId = neigh.Id
		peer.remoteAs = neigh.PeerAs
		curNeigh := neighsStatus[neigh.Id]
		if curNeigh != nil {
			if curNeigh.peer.BgpState != peer.BgpState || curNeigh.peer.remoteIp != peer.remoteIp {
				curNeigh.peer.BgpState = peer.BgpState
				curNeigh.peer.remoteIp = peer.remoteIp
				curNeigh.peer.chgFlag = true
			}
		} else {
			neighsStatus[neigh.Id] =
				&neighborStatus{&peer, &routesInfo{}, false}
			neighsStatus[neigh.Id].peer.chgFlag = true
		}
		neighsStatus[neigh.Id].ageFlag = false
	}

	curNeigh := neighsStatus[neigh.Id]
	if curNeigh != nil {
		if curNeigh.routes != nil {
			if !reflect.DeepEqual(curNeigh.routes, routes) {
				curNeigh.routes = routes
				curNeigh.routes.chgFlag = true
			}
		} else {
			curNeigh.routes.chgFlag = true
			curNeigh.routes = routes
		}
		neighsStatus[neigh.Id].ageFlag = false
	}

	return nil
}

func neighStatusReportCore(neighsStatus map[string]*neighborStatus) error {

	for bgpConfId, status := range neighsStatus {
		if status.peer.chgFlag || status.routes.chgFlag {
			var routes_tpm = &routerinfo{}
			var routes = make([]routerinfo, 0)
			for _, route := range status.routes.Routes {
				for _, tmp := range route {
					routes_tpm.AsPath = tmp.Aspath
					routes_tpm.Network = tmp.Network
					routes_tpm.NextHop = tmp.Nexthops[0].Ip
					routes_tpm.Status = ""
					if tmp.Valid {
						routes_tpm.Status = routes_tpm.Status + "valid,"
					}

					if tmp.Bestpath {
						routes_tpm.Status = routes_tpm.Status + "best,"
					}

				}
				/* 去掉字符末尾逗号 */
				if len(routes_tpm.Status) > 0 {
					routes_tpm.Status = routes_tpm.Status[:len(routes_tpm.Status)-1]
				}

				routes = append(routes, *routes_tpm)
			}

			rptBgpInfo := reportBgpInfo{bgpConfId, status.peer.BgpState, routes}
			bytedata, err := json.Marshal(rptBgpInfo)
			if err != nil {
				agentLog.AgentLogger.Info("[ERROR]Marshal post core err:", err)
				return err
			}

			_, err = public.RequestCore(bytedata, public.G_coreConf.CoreAddress, public.G_coreConf.CorePort, public.G_coreConf.CoreProto, BgpStatusURL)
			if err != nil {
				agentLog.AgentLogger.Info("[ERROR]Marshal post core err:", err, rptBgpInfo)
				///} else {
				//TODO
				status.peer.chgFlag = false
				status.routes.chgFlag = false
			}
		}
	}

	return nil
}

func monitorBgpInfo(ctx context.Context) error {

	neighsStatus := make(map[string]*neighborStatus)

	tick := time.NewTicker(time.Duration(bgpMonitorInterval) * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			// 假如一次获取所有的vrf的bgp邻居和路由可能上报了额外的邻居状态和本地发起的路由，不使用本地数据库信息的话不好做区分
			//var keys = []string{config.BgpConfPath}
			bgpData, _, err := mceetcd.EtcdGetValueWithCheck(config.BgpConfPath)
			if err != nil {
				agentLog.AgentLogger.Error("monitorBgpInfo read vap etcd config failed: %v", err.Error())
				continue
			}

			neighsStatusAgeStart(neighsStatus)

			bgpCfg := &BgpConf{}
			if err = json.Unmarshal([]byte(bgpData), bgpCfg); err != nil {
				///agentLog.AgentLogger.Error("Unmarshal bgpData err: %v ", err)
				continue
			}

			for _, neighb := range bgpCfg.NeighConfig {
				idneigh := strings.Join(CmdShowBgpNeigh("", neighb.PeerAddress), " ")
				idroute := strings.Join(cmdShowBgpRoutes("", neighb.PeerAddress), " ")
				err, neighout := public.ExecBashCmdWithRet(idneigh)
				if err != nil {
					agentLog.AgentLogger.Error("show neigh exec attach err: %s", err)
					continue
				}

				err, routeout := public.ExecBashCmdWithRet(idroute)
				if err != nil {
					agentLog.AgentLogger.Error("show route exec attach err: %s", err)
					continue
				}

				if err := neighsStatusUpdate(&neighb, neighsStatus, neighout, routeout); err != nil {
					agentLog.AgentLogger.Error("neighsStatusUpdate err : %v", err, "ID: ", neighb.Id, "peerIP: ", neighb.PeerAddress)
					continue
				}
			}

			neighsStatusAgeEnd(neighsStatus)
			// 上报控制器状态变化
			neighStatusReportCore(neighsStatus)
		case <-ctx.Done():
			// 必须返回成功
			return nil
		}
	}
}

func InitBgpMonitor() error {
	// 上报bgp邻居状态和路由
	ctx, _ := context.WithCancel(context.Background())
	go func() {
		for {
			defer func() {
				if err := recover(); err != nil {
					/* 设置异常标志 */
					public.G_HeartBeatInfo.Status = "WARNING"
					agentLog.AgentLogger.Error("bgp monitor panic err: %v", err)
				}
			}()

			if err := monitorBgpInfo(ctx); err == nil {
				return
			} else {
				agentLog.AgentLogger.Error("bgp monitor err :%v ", err)
			}
		}
	}()

	return nil
}

func SetBgpPara(monitorInterval int) {
	bgpMonitorInterval = monitorInterval
}

/***
** Update bgp status to core.
** END.
****/

func (conf *BgpConf) Create() error {

	var err error

	/* 如果是空配置，则不创建bgp */
	if conf.LocalAs == 0 {
		return nil
	}

	networks := GetSubnetNetworks()
	conf.Networks = networks

	/* 创建bgp配置 */
	cmd := cmdConfigBgpNeighAll(conf, false)
	err = public.ExecBashCmd(strings.Join(cmd, " "))
	if err != nil {
		agentLog.AgentLogger.Info("Create bgp cmd: ", cmd, err)
		return err
	}

	return nil
}

func (cfgCur *BgpConf) Modify(cfgNew *BgpConf) (error, bool) {

	var chg = false

	/* 如果本端asn改变，则删除重建 */
	if cfgCur.LocalAs != cfgNew.LocalAs {
		/* 先删除旧的neighbor */
		cfgCur.Destroy()

		/* 再创建新的neighbor */
		return cfgNew.Create(), true
	}

	networks := GetSubnetNetworks()
	add, delete := public.Arrcmp(cfgCur.Networks, networks)
	if len(add) != 0 || len(delete) != 0 {
		cfgNew.Networks = networks
		chg = true
	}

	/* 比较配置是否一致 */
	if cfgCur.RouterId != cfgNew.RouterId {
		chg = true
	}

	for _, new := range cfgNew.NeighConfig {
		for _, old := range cfgCur.NeighConfig {
			if new.Id == old.Id {
				if new.PeerAddress != old.PeerAddress ||
					new.PeerAs != old.PeerAs ||
					new.EbgpMutihop != old.EbgpMutihop ||
					new.KeepAlive != old.KeepAlive ||
					new.HoldTime != old.HoldTime ||
					new.MaxPerfix != old.MaxPerfix ||
					new.Password != old.Password {
					chg = true
					break
				}
			}
		}
	}

	for _, old := range cfgCur.NeighConfig {
		for _, new := range cfgNew.NeighConfig {
			if new.Id == old.Id {
				if new.PeerAddress != old.PeerAddress ||
					new.PeerAs != old.PeerAs ||
					new.EbgpMutihop != old.EbgpMutihop ||
					new.KeepAlive != old.KeepAlive ||
					new.HoldTime != old.HoldTime ||
					new.MaxPerfix != old.MaxPerfix ||
					new.Password != old.Password {
					chg = true
					break
				}
			}
		}
	}

	if len(cfgCur.NeighConfig) != len(cfgNew.NeighConfig) {
		chg = true
	}

	if chg {
		/* 修改bgp配置 */
		cmd := cmdModfiyBgpNeigh(cfgCur, cfgNew)
		err := public.ExecBashCmd(strings.Join(cmd, " "))
		if err != nil {
			agentLog.AgentLogger.Info("Modify bgp cmd: ", cmd, err)
			return err, false
		}

		return nil, true
	}

	return nil, false
}

func (conf *BgpConf) Destroy() error {

	cmd := Bgpdel(conf)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	if err != nil {
		return err
	}

	return nil
}

func (conf *BgpConf) ClearNeigh() error {

	cmd := BgpClear(conf)
	err := public.ExecBashCmd(strings.Join(cmd, " "))
	if err != nil {
		return err
	}

	return nil
}
