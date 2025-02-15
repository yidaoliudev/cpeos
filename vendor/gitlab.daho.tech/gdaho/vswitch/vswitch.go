package vswitch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	//"vnetsgw/util/derr"
	"gitlab.daho.tech/gdaho/util/derr"
)

const (
	ovsctl              = "/usr/bin/ovs-vsctl"
	ovsOfctl            = "/usr/bin/ovs-ofctl"
	delBr               = "--if-exists del-br %s"
	showBr              = "show %s -O OpenFlow13"
	iface_to_br         = "iface-to-br %s"
	setControllerMode   = "set controller %s connection_mode=out-of-band"
	getDPID             = "--bare --columns=datapath_id find bridge name=%s"
	getMAC              = "--bare --columns=mac_in_use find interface name=%s"
	getColumn           = "--bare --columns=%s find interface name=%s"
	setController       = "set-controller %s %s"
	delController       = "del-controller %s"
	setFailMode         = "set-fail-mode %s secure"
	getController       = "--bare --columns=_uuid list controller"
	addBr               = "add-br %s"
	delPort             = "del-port %s"
	setPortid           = "-- set interface %s ofport_request=%d"
	addPhyPort          = "add-port %s %s"
	addInternalPort     = "add-port %s %s -- set interface %s type=internal"
	addVxlanPort        = "add-port %s %s -- set interface %s type=%s options:remote_ip=%s options:dst_port=%s options:key=flow"
	listPort            = "--columns=type,ofport,options --format=json find interface name=%s"
	setDPID             = "set bridge %s other-config:datapath-id=%s"
	setBrProto          = "set bridge %s protocols=%s"
	brige_proto_default = "OpenFlow13"
)

const (
	InternalType   = "internal"
	VxlanType      = "vxlan"
	DefaultBridge  = "br0"
	ovs_err_noport = "no port"
)

const (
	OUTOFBAND Mode = "out-of-band"
	INBAND    Mode = "inband"
)

type Mode string

type InterfaceOptions struct {
	RemoteIP string
}

type Interface struct {
	Name     string `json:"portName"`
	Type     string `json:"type"`
	Ofport   int    `json:"ofPort"`
	MAC      string `json:"mac"`
	RemoteIP string `json:"remoteIp"`
	SourceIp string `json:"sourceIp"`
}

func VSwitchGetColumn(port, column string) (string, error) {
	var out, errOutput bytes.Buffer
	args := strings.Split(fmt.Sprintf(getColumn, column, port), " ")
	cmd := exec.Command(ovsctl, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return "", errors.New(errOutput.String())
	}
	info := strings.Split(out.String(), "\n")
	if len(info) > 1 {
		return info[0], nil
	}
	return "", errors.New(fmt.Sprintf("get column %s failed", column))
}

// 判断bridge是否存在，false 不存在
func VSwitchBridgeExist(bridge string) (bool, error) {

	var out, errOutput bytes.Buffer
	args := strings.Split(fmt.Sprintf(showBr, bridge), " ")
	cmd := exec.Command(ovsOfctl, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		if strings.Contains(errOutput.String(), "not a bridge") {
			return false, nil
		}

		return false, errors.New(errOutput.String())
	}

	return true, nil
}

// 判断bridge是否存在，false 不存在
func VSwitchInterfaceExist(intf string) (bool, error) {

	var out, errOutput bytes.Buffer
	args := strings.Split(fmt.Sprintf(iface_to_br, intf), " ")
	cmd := exec.Command(ovsctl, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		if strings.Contains(errOutput.String(), "no interface") {
			return false, nil
		}

		return false, errors.New(errOutput.String())
	} else {
		if strings.Contains(out.String(), DefaultBridge) {
			return true, nil
		}

		return false, nil
	}
}

func VSwitchGetMac() (string, error) {
	mac, err := VSwitchGetColumn(DefaultBridge, "mac_in_use")
	if err != nil {
		return "", err
	}
	return mac, nil
}

func VSwitchGetPortMac(portname string) (string, error) {
	mac, err := VSwitchGetColumn(portname, "mac_in_use")
	if err != nil {
		return "", err
	}
	return mac, nil
}

func VSwitchGetDatapathID() (string, error) {
	var out, errOutput bytes.Buffer
	args := strings.Split(fmt.Sprintf(getDPID, DefaultBridge), " ")
	cmd := exec.Command(ovsctl, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return "", errors.New(errOutput.String())
	}
	info := strings.Split(out.String(), "\n")
	if len(info) > 1 {
		return info[0], nil
	}
	return "", errors.New("get dpid failed")
}

func VSwitchSetDatapathID(datapathid string) error {
	var out, errOutput bytes.Buffer
	args := strings.Split(fmt.Sprintf(setDPID, DefaultBridge, datapathid), " ")
	cmd := exec.Command(ovsctl, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	return nil
}

func VSwitchAddBridge() error {
	VSwitchDelBridge()
	args := strings.Split(fmt.Sprintf(addBr, DefaultBridge), " ")
	cmd := exec.Command(ovsctl, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	args = strings.Split(fmt.Sprintf(setFailMode, DefaultBridge), " ")
	cmd = exec.Command(ovsctl, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	return nil
}

func VSwitchDelBridge() error {
	args := strings.Split(fmt.Sprintf(delBr, DefaultBridge), " ")
	cmd := exec.Command(ovsctl, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	return nil

}

func VSwitchDelControllerIps() error {
	s := fmt.Sprintf(delController, DefaultBridge)
	args := strings.Split(s, " ")
	cmd := exec.Command(ovsctl, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	return nil
}

func VSwitchSetControllers(controllers []string, mode Mode) error {
	s := fmt.Sprintf(setController, DefaultBridge, strings.Join(controllers, " "))
	args := strings.Split(s, " ")
	cmd := exec.Command(ovsctl, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	args = strings.Split(getController, " ")
	cmd = exec.Command(ovsctl, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	controller_uuids := strings.Split(out.String(), "\n")
	for _, uuid := range controller_uuids {
		if uuid == "" {
			continue
		}
		args := strings.Split(fmt.Sprintf(setControllerMode, uuid), " ")
		cmd := exec.Command(ovsctl, args...)
		var out, errOutput bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errOutput
		if err := cmd.Run(); err != nil {
			return errors.New(errOutput.String())
		}
	}
	return nil

}

func VSwitchSetBrProto() error {
	s := fmt.Sprintf(setBrProto, DefaultBridge, brige_proto_default)
	args := strings.Split(s, " ")
	cmd := exec.Command(ovsctl, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}

	return nil

}

/* ovs添加物理端口 */
func (port *Interface) VSwitchAddPhyPort() error {
	s := fmt.Sprintf(addPhyPort, DefaultBridge, port.Name)
	args := strings.Split(s, " ")
	if port.Ofport > 0 {
		s := fmt.Sprintf(setPortid, port.Name, port.Ofport)
		portIDArgs := strings.Split(s, " ")
		args = append(args, portIDArgs...)
	}
	cmd := exec.Command(ovsctl, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}

	if port.Ofport == 0 {
		if err := getPortID(port); err != nil {
			return err
		}
	}

	return nil

}

func getPortID(port *Interface) error {
	ofport, err := VSwitchGetColumn(port.Name, "ofport")
	if err != nil {
		return err
	}
	iofport, err := strconv.Atoi(ofport)
	if err != nil {
		return err
	}
	port.Ofport = iofport
	return nil
}

func (port *Interface) VSwitchAddInternalPort() error {
	s := fmt.Sprintf(addInternalPort, DefaultBridge, port.Name, port.Name)
	args := strings.Split(s, " ")
	if port.Ofport > 0 {
		s := fmt.Sprintf(setPortid, port.Name, port.Ofport)
		portIDArgs := strings.Split(s, " ")
		args = append(args, portIDArgs...)
	}
	cmd := exec.Command(ovsctl, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}

	if port.Ofport == 0 {
		if err := getPortID(port); err != nil {
			return err
		}
	}
	return nil

}

func (port *Interface) VSwitchAddVxlanPort(vxlanport string) error {
	s := fmt.Sprintf(addVxlanPort, DefaultBridge, port.Name, port.Name, VxlanType, port.RemoteIP, vxlanport)
	args := strings.Split(s, " ")
	if port.SourceIp != "" {
		s = fmt.Sprintf("options:local_ip=%s", port.SourceIp)
		args = append(args, s)
	}

	if port.Ofport > 0 {
		s := fmt.Sprintf(setPortid, port.Name, port.Ofport)
		portIDArgs := strings.Split(s, " ")
		args = append(args, portIDArgs...)
	}

	cmd := exec.Command(ovsctl, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return derr.Error{In: errOutput.String(), Out: OVSAddPortError}
	}

	if port.Ofport == 0 {
		if err := getPortID(port); err != nil {
			return err
		}
	}

	return nil

}

func (port *Interface) VSwitchDelPort() error {
	args := strings.Split(fmt.Sprintf(delPort, port.Name), " ")
	cmd := exec.Command(ovsctl, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return derr.Error{In: errOutput.String(), Out: OVSDelPortError}
	}
	return nil
}

// 忽略接口不存在的错误
func (port *Interface) VSwitchDelPortAggressive() error {
	args := strings.Split(fmt.Sprintf(delPort, port.Name), " ")
	cmd := exec.Command(ovsctl, args...)
	var out, errOutput bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		if strings.Contains(errOutput.String(), ovs_err_noport) {
			return nil
		}

		return derr.Error{In: errOutput.String(), Out: OVSDelPortError}
	}
	return nil
}

func (port *Interface) VSwitchCheckPort() error {
	args := strings.Split(fmt.Sprintf(listPort, port.Name), " ")
	cmd := exec.Command(ovsctl, args...)
	var out, errOutput bytes.Buffer
	var target map[string]interface{}
	cmd.Stdout = &out
	cmd.Stderr = &errOutput
	if err := cmd.Run(); err != nil {
		return errors.New(errOutput.String())
	}
	decoder := json.NewDecoder(strings.NewReader(out.String()))
	decoder.UseNumber()
	if err := decoder.Decode(&target); err != nil {
		return errors.New("get port info failed")
	}
	data, ok := target["data"]
	if !ok {
		return errors.New("get port info failed")
	}
	d, ok := data.([]interface{})
	if !ok {
	}
	if len(d) < 1 {
		return errors.New("get port info failed")
	}
	info, ok := d[0].([]interface{})
	if !ok {
		return errors.New("get port info failed")
	}
	if len(info) < 3 {
		return errors.New("get port info failed")
	}
	portType, ok := info[0].(string)
	if !ok {
		return errors.New("get port info failed")
	}
	ofport := info[1].(int)
	if port.Ofport != ofport {
		return errors.New(fmt.Sprintf("ofport wrong %d %d", ofport, port.Ofport))
	}
	options_list := info[2].([]interface{})
	if !ok {
		return errors.New("get port info failed")
	}
	if len(options_list) < 2 {
		return errors.New("get port info failed")
	}
	options, ok := options_list[1].([]interface{})
	if !ok {
		return errors.New("get port info failed")
	}
	if portType == "vxlan" {
		for _, option := range options {
			kv, ok := option.([]interface{})
			if !ok {
				return errors.New("get port info failed")
			}
			if len(kv) < 2 {
				return errors.New("get port info failed")
			}
			k, ok := kv[0].(string)
			if !ok {
				return errors.New("get port info failed")
			}
			if k == "key" {
				v, ok := kv[1].(string)
				if !ok {
					return errors.New("get port info failed")
				}
				if v != "flow" {
					return errors.New("vxlan key wrong")
				}
			} else if k == "remote_ip" {
				v, ok := kv[1].(string)
				if !ok {
					return errors.New("get port info failed")
				}
				if port.RemoteIP != v {
					return errors.New("vxlan remote_ip wrong")
				}
			}
		}
	}
	return nil

}
