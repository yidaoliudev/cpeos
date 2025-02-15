package network

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/template"

	"time"

	"github.com/valyala/fasthttp"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

const (
	DefaultName    = "eth0"
	DefaultGreName = "gre_flow"
	ping           = "ping"
	pingArgs       = "%s -c 1 -W 2"
	ActionAdd      = "add"
	ActionDel      = "del"
	ipcmd          = "ip"
	addlinkgre     = "link add %s type gre local %s remote %s"
	dellink        = "link del %s"
	TableIdMin     = 10 /* 1-9 public net占用 */
	TableIdMax     = 252
	TXQLEN_DEFAULT = 1000
	TCBURST_MIN    = 1600
	ViaHTTP        = 1

	HANDLE_ROOT = netlink.HANDLE_ROOT

	setXfrmState       = "xfrm state add src %s dst %s proto esp spi %s mode %s auth %s %s enc %s %s encap espinudp 4500 4500 0.0.0.0"
	setXfrmPolicy      = "xfrm policy add src %s dst %s dir %s ptype main tmpl src %s dst %s proto esp mode %s"
	delXfrmState       = "xfrm state del src %s dst %s proto esp spi %s"
	delXfrmPolicy      = "xfrm policy del src %s dst %s dir %s"
	defaultRoutePrefix = "0.0.0.0/0"
	defaultHttpTimeOut = 3 * 3
)

const (
	ifcfg = `
BOOTPROTO=static
ONBOOT=yes
{{- if ne .Addr ""}}
IPADDR={{.Addr}}
{{- end}}
{{- if ne .Prefix ""}}
PREFIX={{.Prefix}}
{{- end}}
DEVICE={{.Dev}}
PEERDNS=no
ETHTOOL_OPTS="-K "$DEVICE" tso off; -K "$DEVICE" gso off; -K "$DEVICE" gro off; -K "$DEVICE" lro off; -G "$DEVICE" rx 4096; -G "$DEVICE" tx 4096";
`
	ifroute = `
{{- range .}}
{{.Prefix}} via {{.Nexthop}}
{{- end}}
`

	configPath = "/etc/sysconfig/network-scripts/"
)

var deployType int
var deployAddr string
var deployAddRoutes string
var deployDeleteRoutes string
var deployRollback string

type Route struct {
	Prefix  string `json:"prefix"`
	Nexthop string `json:"nexthop"`
	TableId int    `json:"tableId"`
	Metric  int    `json:"metric"`
	DEV     string `json:"dev"`
}

//func ConfigRule(from string, to string, table int, action string) error {

type Rule struct {
	From    string `json:"from"`
	To      string `json:"to"`
	TableId int    `json:"tableId"`
	Pref    int    `json:"pref",default:"-1"`
}

type NIC struct {
	Addr   string
	Prefix string
	Dev    string
}

type RTTableMgr struct {
	TableId [255]bool //TableIdMin ~ TableIdMax 可用
}

func (table *RTTableMgr) AllocTableId(id int) (int, error) {

	if id != -1 {
		if table.TableId[id] == false {
			table.TableId[id] = true
			return id, nil
		} else {
			return -1, fmt.Errorf("alloc table id %d failed.", id)
		}
	}

	for i := TableIdMin; i <= TableIdMax; i++ {
		if table.TableId[i] == false {
			table.TableId[i] = true
			return i, nil
		}
	}
	return -1, errors.New("alloc table id failed.")
}

func (table *RTTableMgr) ReleaseTableId(tableId int) error {
	if tableId >= TableIdMin && tableId <= TableIdMax {
		table.TableId[tableId] = false
	} else {
		return errors.New("tableid parameter illegality.")
	}

	return nil
}

func (table *RTTableMgr) CheckTableIdInUse(tableId int) (bool, error) {

	if tableId >= TableIdMin && tableId <= TableIdMax {
		return table.TableId[tableId], nil
	}
	return false, errors.New("tableid parameter illegality.")
}

func SetDeployType(t int) {
	deployType = t
}

func SetDeployAddr(addr string) {
	deployAddr = addr
}

func SetDeployAddRoutes(s string) {
	deployAddRoutes = s
}

func SetDeployDeleteRoutes(s string) {
	deployDeleteRoutes = s
}

func SetDeployRollback(s string) {
	deployRollback = s
}

func OvsPortName(uid string) string {
	return uid
}

func VapIdToPort(vapId string) string {
	return "p_" + vapId
}

func GetGreIfName(vapId string) string {
	return "G_" + vapId
}

func GetVxlanIfName(id string) string {
	return "vxlan_" + id
}

func GetVlanIfName(vlanId int, mainIf string) string {
	return mainIf + "." + strconv.Itoa(vlanId)
}

func NSPortName(uid string) string {
	return uid + "_n"
}

func DeleteOvsLink(uid string) error {
	link, err := netlink.LinkByName(OvsPortName(uid))
	if err != nil {
		return err
	}
	err = netlink.LinkDel(link)
	if err != nil {
		return err
	}
	return nil
}

func DeleteLink(linkname string) error {

	/* link不存在返回成功 */
	link, err := netlink.LinkByName(linkname)
	if err == nil {
		err = netlink.LinkDel(link)
		if err != nil {
			return err
		}
	}
	return nil
}
func ConnectedLinks() ([]netlink.Link, error) {
	up_links := make([]netlink.Link, 0)
	links, _ := netlink.LinkList()
	for _, link := range links {
		netlink.LinkSetUp(link)
		link, err := netlink.LinkByIndex(link.Attrs().Index)
		if link.Attrs().OperState != netlink.OperUp {
			continue
		}

		addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
		if err != nil || len(addrs) != 0 {
			continue
		}
		up_links = append(up_links, link)
	}
	if len(up_links) != 2 {
		return up_links, errors.New("too many or too few active nics")
	}
	return up_links, nil
}

/* 通过ping包检查local 和 peer的连通性 */
func CheckIPOnLink(local, peer string, link netlink.Link) bool {
	defer func() {
		AddrFlush(link.Attrs().Name)
	}()
	ip := net.ParseIP(peer)
	if ip == nil {
		return false
	}
	addr, err := netlink.ParseAddr(local)
	if err != nil {
		return false
	}
	err = netlink.AddrAdd(link, addr)
	if err != nil {
		return false
	}
	args := strings.Split(fmt.Sprintf(pingArgs, ip.String()), " ")
	cmd := exec.Command(ping, args...)
	if _, err := cmd.Output(); err != nil {
		netlink.AddrDel(link, addr)
		return false
	}
	return true
}

func LinkSetNS(linkname, usname string) error {
	var link netlink.Link
	link, err := netlink.LinkByName(linkname)
	if err != nil {
		return err
	}

	ns, err := netns.GetFromName(usname)
	if err != nil {
		return err
	}
	defer ns.Close()

	err = netlink.LinkSetNsFd(link, int(ns))
	if err != nil {
		return err
	}

	return nil
}

func LinkSetNSFromDocker(linkname, dockerId string) error {
	var link netlink.Link
	link, err := netlink.LinkByName(linkname)
	if err != nil {
		return err
	}

	ns, err := netns.GetFromDocker(dockerId)
	if err != nil {
		return err
	}
	defer ns.Close()

	err = netlink.LinkSetNsFd(link, int(ns))
	if err != nil {
		return err
	}

	return nil
}

// true = not exist, false = others
func checkRuleOrRouteNotExist(err error) bool {
	if err == nil {
		return false
	}

	if strings.Contains(err.Error(), "no such process") ||
		strings.Contains(err.Error(), "no such file or directory") {
		return true
	}

	return false
}

func doConfigRoute(route *Route, action string) error {
	_, dst, err := net.ParseCIDR(route.Prefix)
	if err != nil {
		return err
	}
	idx, err := GetIdByName(route.DEV)
	if err != nil || idx < 0 {
		idx = 0
	}
	if action == ActionAdd {
		gw := net.ParseIP(route.Nexthop)
		if gw == nil {
			err = netlink.RouteAdd(&netlink.Route{
				LinkIndex: idx,
				Scope:     netlink.SCOPE_UNIVERSE,
				Dst:       dst,
				Table:     route.TableId,
				Priority:  route.Metric,
			})
			// return errors.New("invailed ip format")
		} else {
			err = netlink.RouteAdd(&netlink.Route{
				LinkIndex: idx,
				Scope:     netlink.SCOPE_UNIVERSE,
				Dst:       dst,
				Gw:        gw,
				Table:     route.TableId,
				Priority:  route.Metric,
			})
		}

		if err != nil {
			err = fmt.Errorf("Config route failed, dst %s, gw %s, table %d, metric %d, dev %s: %s",
				route.Prefix,
				route.Nexthop,
				route.TableId,
				route.Metric,
				route.DEV,
				err.Error())
		}

	} else {
		err = netlink.RouteDel(&netlink.Route{
			LinkIndex: idx,
			Scope:     netlink.SCOPE_UNIVERSE,
			Dst:       dst,
			Table:     route.TableId,
			Priority:  route.Metric,
			//	Gw:    gw,
		})

		if err != nil {
			if checkRuleOrRouteNotExist(err) == true {
				return nil
			}

			err = fmt.Errorf("Delete route failed, dst %s, gw %s, table %d, metric %d, dev %s: %s",
				route.Prefix,
				route.Nexthop,
				route.TableId,
				route.Metric,
				route.DEV,
				err.Error())
		}
	}
	if err != nil {
		return err
	}

	return nil
}

func ConfigRoute(route *Route, action string) error {
	if deployType == ViaHTTP {
		var routes []*Route
		routes = append(routes, route)
		jsonBytes, err := json.Marshal(routes)
		if err != nil {
			return err
		}
		var method string
		var url string
		if action == ActionAdd {
			method = "POST"
			url = deployAddr + deployAddRoutes + "?" + deployRollback
		} else {
			method = "DELETE"
			url = deployAddr + deployDeleteRoutes + "?" + deployRollback
		}
		return doRequest(url, string(jsonBytes), method)
	} else {
		return doConfigRoute(route, action)
	}
}

func ConfigRoutes(routes []*Route, action string) error {
	for _, route := range routes {
		ConfigRoute(route, action)
	}

	return nil
}

func ConfigRule(r *Rule, action string) error {

	rule := netlink.NewRule()
	if r.To != "" {
		_, dst, err := net.ParseCIDR(r.To)
		if err != nil {
			return err
		}
		rule.Dst = dst
	}

	if r.From != "" {
		_, src, err := net.ParseCIDR(r.From)
		if err != nil {
			return err
		}
		rule.Src = src
	}
	if r.Pref != -1 {
		rule.Priority = r.Pref
	}

	rule.Table = r.TableId

	var err error
	if action == ActionAdd {
		err = netlink.RuleAdd(rule)
	} else {
		err = netlink.RuleDel(rule)
		if err != nil {
			if checkRuleOrRouteNotExist(err) == true {
				return nil
			}
		}
	}
	if err != nil {
		return err
	}

	return nil
}
func ConfigLinkRouteDev(pref string, tableid int, action string, dev string) error {
	_, dst, err := net.ParseCIDR(pref)
	if err != nil {
		return err
	}
	link, err := netlink.LinkByName(dev)
	if err != nil {
		return err
	}

	route := &netlink.Route{LinkIndex: link.Attrs().Index, Scope: netlink.SCOPE_LINK, Table: tableid, Dst: dst}
	if action == ActionAdd {
		err := netlink.RouteAdd(route)
		if err != nil {
			return err
		}
	} else {
		err := netlink.RouteDel(route)
		if err != nil {
			return err
		}
	}

	return nil

}

func ConfigRouteByDev(routes []Route, action string, dev string) error {
	for _, route := range routes {
		_, dst, err := net.ParseCIDR(route.Prefix)
		if err != nil {
			return err
		}
		if action == ActionAdd {
			link, err := netlink.LinkByName(dev)
			if err != nil {
				return err
			}

			gw := net.ParseIP(route.Nexthop)
			if gw == nil {
				return errors.New("invailed ip format")
			}

			err = netlink.RouteAdd(&netlink.Route{
				Scope:     netlink.SCOPE_UNIVERSE,
				Dst:       dst,
				Gw:        gw,
				LinkIndex: link.Attrs().Index,
			})
		} else {
			err = netlink.RouteDel(&netlink.Route{
				Scope: netlink.SCOPE_UNIVERSE,
				Dst:   dst,
				//	Gw:    gw,
			})
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func AssignIP(nic, ip string) error {
	link, err := netlink.LinkByName(nic)
	if err != nil {
		return err
	}
	addr, err := netlink.ParseAddr(ip)
	if err != nil {
		return err
	}
	err = netlink.AddrAdd(link, addr)
	if err != nil {
		return err
	}
	return nil
}

func DeleteIP(nic, ip string) error {
	link, err := netlink.LinkByName(nic)
	if err != nil {
		return err
	}
	addr, err := netlink.ParseAddr(ip)
	if err != nil {
		return err
	}
	err = netlink.AddrDel(link, addr)
	if err != nil {
		return err
	}
	return nil
}

func DeleteIPLoose(nic, ip string) error {
	link, err := netlink.LinkByName(nic)
	if err != nil {
		return err
	}
	addr, err := netlink.ParseAddr(ip)
	if err != nil {
		return err
	}
	err = netlink.AddrDel(link, addr)
	if err != nil &&
		!strings.Contains(err.Error(), "cannot assign requested address") &&
		!strings.Contains(err.Error(), "Cannot assign requested address") {
		return err
	}

	return nil
}

func NeighAdd(devName, ipAddr, hwAddr string) error {
	link, err := netlink.LinkByName(devName)
	if err != nil {
		return err
	}

	mac, err := net.ParseMAC(hwAddr)
	if err != nil {
		return err
	}

	ip := net.ParseIP(ipAddr)
	if err != nil {
		return err
	}

	err = netlink.NeighAdd(&netlink.Neigh{
		LinkIndex:    link.Attrs().Index,
		State:        netlink.NUD_PERMANENT,
		IP:           ip,
		HardwareAddr: mac,
	})

	return err
}

func NeighSet(devName, ipAddr, hwAddr string) error {
	link, err := netlink.LinkByName(devName)
	if err != nil {
		return err
	}

	mac, err := net.ParseMAC(hwAddr)
	if err != nil {
		return err
	}

	ip := net.ParseIP(ipAddr)
	if err != nil {
		return err
	}

	err = netlink.NeighSet(&netlink.Neigh{
		LinkIndex:    link.Attrs().Index,
		State:        netlink.NUD_PERMANENT,
		IP:           ip,
		HardwareAddr: mac,
	})

	return err
}

func NeighDel(devName, ipAddr, hwAddr string) error {

	var mac net.HardwareAddr

	link, err := netlink.LinkByName(devName)
	if err != nil {
		return err
	}

	ip := net.ParseIP(ipAddr)
	if err != nil {
		return err
	}

	if hwAddr != "" {
		if mac, err = net.ParseMAC(hwAddr); err != nil {
			return err
		}
	}

	err = netlink.NeighDel(&netlink.Neigh{
		LinkIndex:    link.Attrs().Index,
		State:        netlink.NUD_PERMANENT,
		IP:           ip,
		HardwareAddr: mac,
	})

	return err
}
func AddrFlush(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return err
	}
	for _, addr := range addrs {
		err := netlink.AddrDel(link, &addr)
		if err != nil {
			return err
		}
	}
	return nil

}

func LinkDownAndUp(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}

	err = netlink.LinkSetDown(link)
	if err != nil {
		return err
	}
	err = netlink.LinkSetUp(link)
	if err != nil {
		return err
	}
	return nil
}
func SetLinkUp(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}

	err = netlink.LinkSetUp(link)
	if err != nil {
		return err
	}
	return nil
}

func SetLinkMtu(name string, mtu int) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}
	err = netlink.LinkSetMTU(link, mtu)
	if err != nil {
		return err
	}
	return err
}

func InitLink(uid, mac string) error {
	name := NSPortName(uid)
	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}

	hwaddr, err := net.ParseMAC(mac)
	if err != nil {
		return err
	}

	err = netlink.LinkSetHardwareAddr(link, hwaddr)
	if err != nil {
		return err
	}

	err = netlink.LinkSetName(link, DefaultName)
	if err != nil {
		return err
	}
	err = netlink.LinkSetUp(link)
	if err != nil {
		return err
	}
	return nil
}

func SetPortMac(portname string, mac string) error {

	link, err := netlink.LinkByName(portname)
	if err != nil {
		return err
	}

	hwaddr, err := net.ParseMAC(mac)
	if err != nil {
		return err
	}

	err = netlink.LinkSetHardwareAddr(link, hwaddr)
	if err != nil {
		return err
	}

	return nil
}

func GetPortMac(portname string) (error, string) {

	link, err := netlink.LinkByName(portname)
	if err != nil {
		return err, ""
	}

	return nil, link.Attrs().HardwareAddr.String()
}

func DeleteVlanLink(vlanID int, ifName string) error {
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return err
	}
	linkAttrs := netlink.NewLinkAttrs()
	name := ifName + "." + strconv.Itoa(vlanID)
	linkAttrs.Name = name
	linkAttrs.ParentIndex = link.Attrs().Index
	vlan := &netlink.Vlan{linkAttrs, vlanID}
	err = netlink.LinkDel(vlan)
	if err != nil {
		return err
	}
	return nil
}

func NewVlanLink(vlanID int, ip string, ifName string) error {
	if vlanID == 0 {
		return errors.New("vlan id must greater than 0")
	}
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return err
	}

	linkAttrs := netlink.NewLinkAttrs()
	name := ifName + "." + strconv.Itoa(vlanID)
	linkAttrs.Name = name
	linkAttrs.ParentIndex = link.Attrs().Index
	vlan := &netlink.Vlan{linkAttrs, vlanID}
	err = netlink.LinkAdd(vlan)
	if err != nil {
		return err
	}
	err = netlink.LinkSetUp(vlan)
	if err != nil {
		netlink.LinkDel(vlan)
		return err
	}

	addr, err := netlink.ParseAddr(ip)
	if err != nil {
		netlink.LinkDel(vlan)
		return err
	}
	err = netlink.AddrAdd(vlan, addr)
	if err != nil {
		netlink.LinkDel(vlan)
		return err
	}
	return nil
}

func CreateVlanLink(vlanID int, ifName string) error {
	if vlanID == 0 {
		return errors.New("vlan id must greater than 0")
	}
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return err
	}

	linkAttrs := netlink.NewLinkAttrs()
	name := ifName + "." + strconv.Itoa(vlanID)
	linkAttrs.Name = name
	linkAttrs.ParentIndex = link.Attrs().Index
	vlan := &netlink.Vlan{linkAttrs, vlanID}
	err = netlink.LinkAdd(vlan)
	if err != nil {
		return err
	}
	return nil
}

func NewVethLink(name string, peer string) error {

	linkAttrs := netlink.NewLinkAttrs()
	linkAttrs.Name = name
	veth := &netlink.Veth{linkAttrs, peer}
	err := netlink.LinkAdd(veth)
	if err != nil {
		return err
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}

	err = netlink.LinkSetUp(link)
	if err != nil {
		return err
	}

	link, err = netlink.LinkByName(peer)
	if err != nil {
		return err
	}

	err = netlink.LinkSetUp(link)
	if err != nil {
		return err
	}
	return nil
}

func DelDefaultGreLink() error {
	err := DeleteLink(DefaultGreName)
	return err
}

func NewDefaultGreLink(localIp string, remoteIp string) error {

	args := strings.Split(fmt.Sprintf(addlinkgre, DefaultGreName, localIp, remoteIp), " ")
	cmd := exec.Command(ipcmd, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func NewGreLink(localIp string, remoteIp string, greName string) error {

	args := strings.Split(fmt.Sprintf(addlinkgre, greName, localIp, remoteIp), " ")
	cmd := exec.Command(ipcmd, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}

	return nil
}

func NewIfbLink(ifbName string) error {

	linkAttrs := netlink.NewLinkAttrs()
	linkAttrs.Name = ifbName
	ifb := &netlink.Ifb{linkAttrs}
	err := netlink.LinkAdd(ifb)
	if err != nil {
		return err
	}

	link, err := netlink.LinkByName(ifbName)
	if err != nil {
		return err
	}

	err = netlink.LinkSetUp(link)
	if err != nil {
		return err
	}

	return nil
}
func PersistentNetwork(eths []NIC) error {
	for idx, eth := range eths {
		tmpl, err := template.New(eth.Dev).Parse(ifcfg)
		if err != nil {
			return err
		}

		temp, err := ioutil.TempFile(filepath.Dir(configPath), "."+strconv.Itoa(idx))

		if err != nil {
			return err
		}

		if err = tmpl.Execute(temp, eth); err != nil {
			return err
		}
		err = os.Rename(temp.Name(), path.Join(configPath, "ifcfg-"+eth.Dev))
		if err != nil {
			return err
		}
		temp.Close()

	}
	return nil
}

func PersistentRoutes(routes []Route, name string) error {

	tmpl, err := template.New("route").Parse(ifroute)
	if err != nil {
		return err
	}

	temp, err := ioutil.TempFile(filepath.Dir(configPath), "."+"route")

	if err != nil {
		return err
	}

	if err = tmpl.Execute(temp, routes); err != nil {
		return err
	}
	err = os.Rename(temp.Name(), path.Join(configPath, "route-"+name))
	if err != nil {
		return err
	}
	temp.Close()
	return nil
}

func IsLinkExisted(linkname string) bool {

	_, err := netlink.LinkByName(linkname)
	if err != nil {
		return false
	}

	return true
}

/* ipaddr link 1.1.1.1/24 */
func CheckIpAddrIsExisted(ipaddr string) bool {

	vals, _ := net.InterfaceAddrs()

	for _, val := range vals {
		addr, _ := val.(*net.IPNet)
		if ipaddr == addr.String() {
			return true
		}
	}

	return false
}

/* 检查去掉掩码后的地址是否存在，创建xvlan时不关注掩码 */
func CheckIpAddrWithOutMaskIsExisted(ipaddr string) bool {

	vals, _ := net.InterfaceAddrs()
	tmp := strings.Split(ipaddr, "/")

	for _, val := range vals {
		addr, _ := val.(*net.IPNet)
		tmpCur := strings.Split(addr.String(), "/")
		if tmp[0] == tmpCur[0] {
			return true
		}
	}

	return false
}

func CheckIpIsExistedSpecif(ifName string, ipaddr string) (bool, error) {

	ifi, err := net.InterfaceByName(ifName)
	if err != nil {
		return false, err
	}

	ips, err := ifi.Addrs()
	if err != nil {
		return false, err
	}

	for _, ip := range ips {
		addr, _ := ip.(*net.IPNet)
		if ipaddr == addr.String() {
			return true, nil
		}
	}
	return false, nil
}

func MacIsEqual(mac1 net.HardwareAddr, mac2 net.HardwareAddr) bool {
	if mac1[0] == mac2[0] &&
		mac1[1] == mac2[0] &&
		mac1[2] == mac2[0] &&
		mac1[3] == mac2[0] &&
		mac1[4] == mac2[0] &&
		mac1[5] == mac2[0] {
		return true
	}
	return false
}

func MacIsBroadcast(mac net.HardwareAddr) bool {
	if mac[0] == 0xFF &&
		mac[1] == 0xFF &&
		mac[2] == 0xFF &&
		mac[3] == 0xFF &&
		mac[4] == 0xFF &&
		mac[5] == 0xFF {
		return true
	}
	return false
}

func MacIsMulticast(mac net.HardwareAddr) bool {
	if mac[0]&0x01 != 0 {
		return true
	}
	return false
}

func incMacByte(macb *byte) bool {
	if *macb == 0xFF {
		*macb = 0
		return true
	} else {
		*macb = *macb + 1
		return false
	}
}
func MacIncrease(mac net.HardwareAddr) net.HardwareAddr {
	var incflag = true
	macInc := mac

	for i := len(macInc) - 1; i >= 0; i-- {
		incflag = incMacByte(&macInc[i])
		if incflag == false {
			if MacIsMulticast(macInc) {
				macInc[0] = macInc[0] + 1
			} else if MacIsBroadcast(macInc) {
				return net.HardwareAddr([]byte{00, 00, 00, 00, 00, 01})
			} else {
				return macInc
			}
		}
	}

	if true == incflag {
		return net.HardwareAddr([]byte{00, 00, 00, 00, 00, 01})
	}

	return macInc
}

/* rate单位bit/s */
func getBurstFromRate(rate uint64) uint32 {

	/* netlink中自带的计算HZ的算法有bug，先按照HZ=100计算 */
	burst := rate / 800
	if burst < TCBURST_MIN {
		burst = TCBURST_MIN
	}
	return uint32(burst)
}

func HtbQdiscAdd(port string, qdisHd uint32, parent uint32, defCls uint32) error {

	link, err := netlink.LinkByName(port)
	if err != nil {
		return err
	}
	// add htb qdisc
	attrs := netlink.QdiscAttrs{LinkIndex: link.Attrs().Index,
		Parent: parent,
		Handle: qdisHd,
	}
	htb := netlink.NewHtb(attrs)
	htb.Defcls = defCls
	err = netlink.QdiscAdd(htb)
	if err != nil {
		return err
	}

	return nil
}

func HtbQdiscDel(port string, qdiscHd uint32) error {

	link, err := netlink.LinkByName(port)
	if err != nil {
		return err
	}
	// add htb qdisc
	attrs := netlink.QdiscAttrs{LinkIndex: link.Attrs().Index,
		Parent: netlink.HANDLE_ROOT,
		Handle: qdiscHd,
	}
	htb := netlink.NewHtb(attrs)
	err = netlink.QdiscDel(htb)
	if err != nil {
		return err
	}

	return nil
}

func SfqQdiscAdd(port string, parentHd uint32, qdisHd uint32, perturb int32) error {

	link, err := netlink.LinkByName(port)
	if err != nil {
		return err
	}

	// add htb qdisc
	attrs := netlink.QdiscAttrs{LinkIndex: link.Attrs().Index,
		Parent: parentHd,
		Handle: qdisHd,
	}
	sfq := netlink.NewSfq(attrs)
	sfq.Perturb = perturb
	err = netlink.QdiscAdd(sfq)
	if err != nil {
		return err
	}

	return nil
}

func SfqQdiscDel(port string, parentHd uint32, qdisHd uint32) error {

	link, err := netlink.LinkByName(port)
	if err != nil {
		return err
	}
	// add htb qdisc
	attrs := netlink.QdiscAttrs{LinkIndex: link.Attrs().Index,
		Parent: parentHd,
		Handle: qdisHd, // 使用默认值
	}

	sfq := netlink.NewSfq(attrs)
	err = netlink.QdiscDel(sfq)
	if err != nil {
		return err
	}

	return nil
}

func IfbQdiscAdd(interPort string) error {
	link, err := netlink.LinkByName(interPort)
	if err != nil {
		return err
	}

	qdisc := &netlink.Ingress{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.HANDLE_INGRESS,
		},
	}

	err = netlink.QdiscAdd(qdisc)
	if err != nil {
		return err
	}
	return nil
}

func IfbQdiscDel(interPort string) error {
	link, err := netlink.LinkByName(interPort)
	if err != nil {
		return err
	}

	qdisc := &netlink.Ingress{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.HANDLE_INGRESS,
		},
	}

	err = netlink.QdiscDel(qdisc)
	if err != nil {
		return err
	}
	return nil
}

func IfbFilterAdd(interPort string, ifbPort string) error {
	linkInter, err := netlink.LinkByName(interPort)
	if err != nil {
		return err
	}
	linkIfb, err := netlink.LinkByName(ifbPort)
	if err != nil {
		return err
	}
	filter := &netlink.U32{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: linkInter.Attrs().Index,
			Parent:    netlink.MakeHandle(0xffff, 0),
			Protocol:  unix.ETH_P_ALL,
		},
		Actions: []netlink.Action{
			&netlink.MirredAction{
				ActionAttrs: netlink.ActionAttrs{
					Action: netlink.TC_ACT_STOLEN,
				},
				MirredAction: netlink.TCA_EGRESS_REDIR,
				Ifindex:      linkIfb.Attrs().Index,
			},
		},
	}

	if err := netlink.FilterAdd(filter); err != nil {
		return err
	}
	return nil
}

func IfbFilterDel(interPort string, ifbPort string) error {
	linkInter, err := netlink.LinkByName(interPort)
	if err != nil {
		return err
	}
	linkIfb, err := netlink.LinkByName(ifbPort)
	if err != nil {
		return err
	}
	filter := &netlink.U32{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: linkInter.Attrs().Index,
			Parent:    netlink.MakeHandle(0xffff, 0),
			Protocol:  unix.ETH_P_ALL,
		},
		Actions: []netlink.Action{
			&netlink.MirredAction{
				ActionAttrs: netlink.ActionAttrs{
					Action: netlink.TC_ACT_STOLEN,
				},
				MirredAction: netlink.TCA_EGRESS_REDIR,
				Ifindex:      linkIfb.Attrs().Index,
			},
		},
	}

	if err := netlink.FilterDel(filter); err != nil {
		return err
	}
	return nil
}

func HtbClassAdd(port string, parent uint32, clsHd uint32, rate uint64, prio uint32) error {

	link, err := netlink.LinkByName(port)
	if err != nil {
		return err
	}

	// add class
	clsAttrs := netlink.ClassAttrs{LinkIndex: link.Attrs().Index,
		Parent: parent,
		Handle: clsHd,
	}
	//为了防止brust过小导致突发流量被丢掉，经测试调成6倍
	burst := getBurstFromRate(rate * 6)
	htbclsAttrs := netlink.HtbClassAttrs{Rate: rate, Buffer: burst, Cbuffer: burst, Prio: prio}

	htbCls := netlink.NewHtbClass(clsAttrs, htbclsAttrs)
	err = netlink.ClassAdd(htbCls)
	if err != nil {
		return err
	}
	return nil
}

func HtbClassDel(port string, qdiscHd uint32, clsHd uint32) error {

	link, err := netlink.LinkByName(port)
	if err != nil {
		return err
	}

	// add class
	clsAttrs := netlink.ClassAttrs{LinkIndex: link.Attrs().Index,
		Parent: qdiscHd,
		Handle: clsHd,
	}

	htbclsAttrs := netlink.HtbClassAttrs{}
	htbCls := netlink.NewHtbClass(clsAttrs, htbclsAttrs)
	err = netlink.ClassDel(htbCls)
	if err != nil {
		return err
	}
	return nil
}

func HtbClassChg(port string, qdiscHd uint32, clsHd uint32, rate uint64, prio uint32) error {

	link, err := netlink.LinkByName(port)
	if err != nil {
		return err
	}

	// add class
	clsAttrs := netlink.ClassAttrs{LinkIndex: link.Attrs().Index,
		Parent: qdiscHd,
		Handle: clsHd,
	}
	//为了防止brust过小导致突发流量被丢掉，经测试调成6倍
	burst := getBurstFromRate(rate * 6)
	htbclsAttrs := netlink.HtbClassAttrs{Rate: rate, Buffer: burst, Cbuffer: burst, Prio: prio}

	htbCls := netlink.NewHtbClass(clsAttrs, htbclsAttrs)
	err = netlink.ClassChange(htbCls)
	if err != nil {
		return err
	}
	return nil
}

func HtbFilterDel(port string, qdiscHd uint32, clsHd uint32) error {
	link, err := netlink.LinkByName(port)
	if err != nil {
		return err
	}

	filter := &netlink.U32{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    qdiscHd,
			Priority:  1,
			Protocol:  unix.ETH_P_IP,
		},
		ClassId: clsHd,
	}
	if err := netlink.FilterDel(filter); err != nil {
		return err
	}
	return nil
}

func Inet_ntoa(ipnr uint32) string {
	var bytes [4]byte
	bytes[0] = byte(ipnr & 0xFF)
	bytes[1] = byte((ipnr >> 8) & 0xFF)
	bytes[2] = byte((ipnr >> 16) & 0xFF)
	bytes[3] = byte((ipnr >> 24) & 0xFF)

	ip := net.IPv4(bytes[3], bytes[2], bytes[1], bytes[0])
	return ip.String()
}

func Inet_aton(ipstr string) uint32 {
	ip := net.ParseIP(ipstr)

	var sum uint32

	sum += uint32(ip[12]) << 24
	sum += uint32(ip[13]) << 16
	sum += uint32(ip[14]) << 8
	sum += uint32(ip[15])

	return sum
}

func Ipmask2Ip(ipstr string) string {
	ipSegs := strings.Split(ipstr, "/")
	return ipSegs[0]
}

const (
	MATCHTYPE_IP_SRCIP = iota
	MATCHTYPE_IP_DSTIP
	MATCHTYPE_IP_PROTOCOL
	MATCHTYPE_IP_SRCPORT
	MATCHTYPE_IP_DSTPORT
	MATCHTYPE_TCP_SRCPORT // 需要设置OffMask，非ip的match项需要测试是否支持
	MATCHTYPE_TCP_DSTPORT
	MATCHTYPE_UDP_SRCPORT
	MATCHTYPE_UDP_DSTPORT
)

type U32FilterMatch struct {
	MatchType int
	Value     uint32
	Mask      uint32
}

func U32FilterGetSelKey(matchs []U32FilterMatch) ([]netlink.TcU32Key, error) {

	u32SelKeys := []netlink.TcU32Key{}
	u32SelKey := netlink.TcU32Key{}

	for _, match := range matchs {
		switch match.MatchType {
		case MATCHTYPE_IP_SRCIP:
			u32SelKey = netlink.TcU32Key{
				Mask:    match.Mask,
				Val:     match.Value,
				Off:     12,
				OffMask: 0,
			}
		case MATCHTYPE_IP_DSTIP:
			u32SelKey = netlink.TcU32Key{
				Mask:    match.Mask,
				Val:     match.Value,
				Off:     16,
				OffMask: 0,
			}
		case MATCHTYPE_IP_PROTOCOL:
			u32SelKey = netlink.TcU32Key{
				Mask:    0x00ff0000,
				Val:     match.Value << 16,
				Off:     8,
				OffMask: 0,
			}
		case MATCHTYPE_IP_SRCPORT:
			u32SelKey = netlink.TcU32Key{
				Mask:    0xffff0000,
				Val:     match.Value << 16,
				Off:     20,
				OffMask: 0,
			}
		case MATCHTYPE_IP_DSTPORT:
			u32SelKey = netlink.TcU32Key{
				Mask:    0x0000ffff,
				Val:     match.Value,
				Off:     20,
				OffMask: 0,
			}
		default:
			return u32SelKeys, errors.New("not support u32 match type: " + strconv.Itoa(match.MatchType))
		}

		u32SelKeys = append(u32SelKeys, u32SelKey)
	}

	return u32SelKeys, nil
}

func U32FilterAdd(port string,
	parent uint32,
	classId uint32,
	prio uint16,
	keys []netlink.TcU32Key) error {

	link, err := netlink.LinkByName(port)
	if err != nil {
		return err
	}
	filter := &netlink.U32{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    parent,
			Priority:  prio,
			Protocol:  unix.ETH_P_IP,
		},
		Sel: &netlink.TcU32Sel{
			Keys:  keys,
			Flags: netlink.TC_U32_TERMINAL,
		},
		ClassId: classId,
		Actions: []netlink.Action{},
	}

	return netlink.FilterAdd(filter)
}

func U32FilterDel(port string,
	handle uint32,
	parent uint32,
	classId uint32,
	prio uint16,
	keys []netlink.TcU32Key) error {

	link, err := netlink.LinkByName(port)
	if err != nil {
		return err
	}
	filter := &netlink.U32{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    handle,
			Parent:    parent,
			Priority:  prio,
			Protocol:  unix.ETH_P_IP,
		},
		Sel: &netlink.TcU32Sel{
			Keys:  keys,
			Flags: netlink.TC_U32_TERMINAL,
		},
		ClassId: classId,
		Actions: []netlink.Action{},
	}

	return netlink.FilterDel(filter)
}

func GetU32FilterHander(dev string, parent uint32, chassid uint32, prio uint16) (error, uint32, uint16) {
	var handle uint32 = 0

	link, err := netlink.LinkByName(dev)
	if err != nil {
		return err, handle, 0
	}

	filters, err := netlink.FilterList(link, parent)
	for _, filter := range filters {
		switch filter := filter.(type) {
		case *netlink.U32:
			if filter.ClassId == chassid {
				return nil, filter.Handle, filter.Priority
			}
		}
	}

	return errors.New("no such filter"), handle, 0
}

func ConfigPortTxQLen(port string, qLen int) error {

	link, err := netlink.LinkByName(port)
	if err != nil {
		return err
	}

	err = netlink.LinkSetTxQLen(link, qLen)
	if err != nil {
		return err
	}

	return nil
}

/* rate 单位为bit/s
func PortRateLimitWithHTB(port string, rate uint64) error {

	link, err := netlink.LinkByName(port)
	if err != nil {
		return err
	}
	// add htb qdisc
	attrs := netlink.QdiscAttrs{LinkIndex: link.Attrs().Index,
		Parent: netlink.HANDLE_ROOT,
		Handle: QDISC_DEFAULTHANBDLE,
	}
	htb := netlink.NewHtb(attrs)
	htb.Defcls = QDISC_DEFAULTCLS
	err = netlink.QdiscAdd(htb)
	if err != nil {
		return err
	}

	// add class
	clsAttrs := netlink.ClassAttrs{LinkIndex: link.Attrs().Index,
		Parent: QDISC_DEFAULTHANBDLE,
		Handle: CLASS_DEFAULTHANBDLE,
	}

	burst := getBurstFromRate(rate)
	htbclsAttrs := netlink.HtbClassAttrs{Rate: rate, Buffer: burst, Cbuffer: burst, Prio: TCPRIO_DEFAULTFLOW}

	htbCls := netlink.NewHtbClass(clsAttrs, htbclsAttrs)
	err = netlink.ClassAdd(htbCls)
	if err != nil {
		netlink.QdiscDel(htb)
		return err
	}
	return nil
}
*/

func MakeTcHandle(major, minor uint16) uint32 {
	return netlink.MakeHandle(major, minor)
}

func RuleList(pref int, from string) (bool, error) {
	var exist bool = false
	rules, err := netlink.RuleList(syscall.AF_INET)
	if err != nil {
		return exist, err
	}

	ip, _, err := net.ParseCIDR(from)
	if err != nil {
		return false, err
	}

	for _, rule := range rules {
		if pref == rule.Priority {
			if rule.Src == nil {
				continue
			}
			if rule.Src.Contains(ip) {
				exist = true
				break
			} else {
				continue
			}
		} else {
			continue
		}
	}
	return exist, nil
}

func GetDefaultRoutes() ([]netlink.Route, error) {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Dst: nil, Table: unix.RT_TABLE_MAIN}, netlink.RT_FILTER_DST)
	if err != nil {
		return nil, err
	}

	return routes, nil
}

func GetDefaultRoute() (string, error) {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Dst: nil, Table: unix.RT_TABLE_MAIN, Priority: 0}, netlink.RT_FILTER_DST)
	if err != nil {
		return "", err
	}

	if len(routes) > 0 {
		for _, rt := range routes {
			if rt.Table == unix.RT_TABLE_MAIN && rt.Gw != nil && rt.Priority == 0 {
				return rt.Gw.String(), nil
			}
		}
	}

	return "", nil
}

func GetRouteSrcByGw(gw string) (string, error) {
	filter := &netlink.Route{Dst: nil, Table: unix.RT_TABLE_UNSPEC, Gw: net.IP(gw)}
	filter.Table = unix.RT_TABLE_UNSPEC
	filter.Gw = net.ParseIP(gw)

	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, filter, netlink.RT_FILTER_GW|netlink.RT_FILTER_TABLE)
	if err != nil {
		return "", err
	}

	if len(routes) > 0 {
		ifIndex := routes[0].LinkIndex

		lk, err := netlink.LinkByIndex(ifIndex)
		if err != nil {
			return "", err
		}

		addrList, err := netlink.AddrList(lk, netlink.FAMILY_V4)
		if err != nil {
			return "", err
		}

		if len(addrList) == 0 {
			return "", errors.New("route port no ip addr.")
		}

		ip := addrList[0].IPNet.String()[:strings.IndexByte(addrList[0].IPNet.String(), '/')]
		return ip, nil
	}

	return "", errors.New("no route match with the gw " + gw)
}

func GetDefaultRouteByMetric(metric int) (string, error) {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Dst: nil, Table: unix.RT_TABLE_MAIN, Priority: 0}, netlink.RT_FILTER_DST)
	if err != nil {
		return "", err
	}

	if len(routes) > 0 {
		for _, rt := range routes {
			if rt.Table == unix.RT_TABLE_MAIN && rt.Gw != nil && rt.Priority == metric {
				return rt.Gw.String(), nil
			}
		}
	}

	return "", nil
}

func DelDefaultRoute() error {
	defaultroute, err := GetDefaultRoute()
	if err != nil {
		return err
	}

	if defaultroute == "" {
		return nil
	} else {
		route := &Route{Prefix: "0.0.0.0/0", TableId: unix.RT_TABLE_MAIN, Metric: 0}
		if err = ConfigRoute(route, ActionDel); err != nil {
			return err
		}
	}
	return nil
}

func GetDefaultRouteDev() (string, error) {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Dst: nil}, netlink.RT_FILTER_DST)
	if err != nil {
		return "", err
	}

	if len(routes) > 0 {
		for _, rt := range routes {
			if rt.Table == unix.RT_TABLE_MAIN && rt.Gw != nil {
				lk, err := netlink.LinkByIndex(rt.LinkIndex)
				if err != nil {
					return "", err
				}
				return lk.Attrs().Name, nil
				//return rt.Gw.String(), nil
			}
		}
	}

	return "", nil
}

func GetRouteDev(route *Route) (exist bool, portName string, err error) {

	filter := &netlink.Route{Table: route.TableId, Gw: net.ParseIP(route.Nexthop)}
	var filterMask uint64
	/*默认路由的dst为nil*/
	if route.Prefix != "0.0.0.0/0" {
		_, filter.Dst, err = net.ParseCIDR(route.Prefix)
		if err != nil {
			return false, "", fmt.Errorf("ParseCIDR %v from prefix err: %v", route.Prefix, err)
		}
		filterMask = netlink.RT_FILTER_TABLE | netlink.RT_FILTER_DST | netlink.RT_FILTER_GW
	} else {
		filterMask = netlink.RT_FILTER_TABLE | netlink.RT_FILTER_DST
	}

	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, filter, filterMask)
	if err != nil {
		return false, "", err
	}

	if len(routes) > 0 {
		for _, rt := range routes {
			if rt.Priority == route.Metric {
				lk, err := netlink.LinkByIndex(rt.LinkIndex)
				if err != nil {
					return false, "", err
				}
				return true, lk.Attrs().Name, nil
			}
		}
	}

	return false, "", nil
}

func doRequest(url string, body string, method string) (err error) {

	req := fasthttp.AcquireRequest()
	req.SetRequestURI(url)
	req.Header.SetContentType("application/json")
	req.Header.SetMethod(method)
	req.SetBodyString(body)

	resp := fasthttp.AcquireResponse()
	client := &fasthttp.Client{}
	//client.Do(req, resp)
	client.DoTimeout(req, resp, time.Duration(defaultHttpTimeOut*time.Second))
	if len(resp.Body()) != 0 {
		var result map[string]interface{}
		json.Unmarshal(resp.Body(), &result)
		message := result["message"]
		res := result["result"]
		if message != nil && res != nil {
			if res.(string) == "success" {
				return nil
			} else if res.(string) == "fail" {
				err = fmt.Errorf("http response error - %s", message.(string))
				return err
			}
		} else {
			err = fmt.Errorf("unknown error - %v", result)
		}
	} else {
		err = fmt.Errorf("unknown error - %v", resp)
	}
	return err
}

func GetNameByIndex(index int) (string, error) {
	link, err := netlink.LinkByIndex(index)
	if err != nil {
		return "", err
	}

	return link.Attrs().Name, nil
}

func GetIdByName(name string) (int, error) {
	lk, err := netlink.LinkByName(name)
	if err != nil {
		return -1, err
	}

	return lk.Attrs().Index, nil
}

func DelFlsuhTableRoute(tableid int) error {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Table: tableid}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return err
	}

	for _, route := range routes {
		switch route.Scope {
		case netlink.SCOPE_UNIVERSE:
			rt := &Route{}
			if route.Dst == nil {
				rt = &Route{Prefix: defaultRoutePrefix, TableId: route.Table, Nexthop: route.Gw.String(), Metric: route.Priority}
			} else {
				rt = &Route{Prefix: route.Dst.String(), TableId: route.Table, Nexthop: route.Gw.String(), Metric: route.Priority}
			}
			err = ConfigRoute(rt, ActionDel)
			if err != nil {
				return err
			}
		case netlink.SCOPE_LINK:
			if err := netlink.RouteDel(&route); err != nil {
				return errors.New(fmt.Sprintf("RouteDel SCOPE_LINK route %v err: %v", route, err))
			}
		}
	}

	return nil
}

type XfrmState struct {
	Src     string
	Dst     string
	Spi     string
	Mode    string
	Auth    string
	AuthKey string
	Enc     string
	EncKey  string
}

type XfrmPolicy struct {
	Src     string
	Dst     string
	Dir     string
	TmplSrc string
	TmplDst string
	Mode    string
}

func AddXfrmPolicy(xfrmPolicy *XfrmPolicy) error {
	var out, errOutput bytes.Buffer
	args := strings.Split(fmt.Sprintf(setXfrmPolicy, xfrmPolicy.Src, xfrmPolicy.Dst,
		xfrmPolicy.Dir, xfrmPolicy.TmplSrc, xfrmPolicy.TmplDst, xfrmPolicy.Mode), " ")
	cmd := exec.Command(ipcmd, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	return nil
}

func AddXfrmState(xfrmState *XfrmState) error {
	var out, errOutput bytes.Buffer
	args := strings.Split(fmt.Sprintf(setXfrmState, xfrmState.Src, xfrmState.Dst,
		xfrmState.Spi, xfrmState.Mode, xfrmState.Auth, xfrmState.AuthKey, xfrmState.Enc, xfrmState.EncKey), " ")
	cmd := exec.Command(ipcmd, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	return nil
}

func DelXfrmState(xfrmState *XfrmState) error {
	var out, errOutput bytes.Buffer
	args := strings.Split(fmt.Sprintf(delXfrmState, xfrmState.Src, xfrmState.Dst, xfrmState.Spi), " ")
	cmd := exec.Command(ipcmd, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	return nil
}

func DelXfrmPolicy(xfrmPolicy *XfrmPolicy) error {
	var out, errOutput bytes.Buffer
	args := strings.Split(fmt.Sprintf(delXfrmPolicy, xfrmPolicy.Src, xfrmPolicy.Dst, xfrmPolicy.Dir), " ")
	cmd := exec.Command(ipcmd, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	return nil
}

func LinkGetRxTXBytes(devName string) (uint64, uint64, error) {
	link, err := netlink.LinkByName(devName)
	if err != nil {
		return 0, 0, err
	}

	Stats := link.Attrs().Statistics

	return Stats.RxBytes, Stats.TxBytes, nil
}

func GetPortPhyStatusByName(name string) (bool, error) {

	link, err := netlink.LinkByName(name)
	if err != nil {
		return false, err
	}

	// 虚拟设备可能是 Unknown 状态
	if link.Attrs().OperState == netlink.OperUp ||
		(link.Attrs().OperState == netlink.OperUnknown && link.Attrs().Flags&1 != 0) {
		return true, nil
	} else {
		return false, nil
	}
}
