package main

import (
	"cpeos/agentLog"
	"cpeos/app"
	"cpeos/config"
	mceetcd "cpeos/etcd"
	"cpeos/public"
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"gitlab.daho.tech/gdaho/etcd"
	"gitlab.daho.tech/gdaho/network"
	"gitlab.daho.tech/gdaho/util/derr"
)

var (
	etcdClient   *etcd.Client
	BackendNodes []string = []string{"http://127.0.0.1:2379"}
	logName               = "/var/log/cpe/recover.log"
)

/* 	业务恢复之前准备 */
func proRecover() error {
	var err error

	/* 配置转发开发 */
	cmdstr := "sysctl -w net.ipv4.ip_forward=1"
	err, _ = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info(cmdstr, err)
	}

	/* 关闭方向路由检查 */
	cmdstr = "sysctl -w net.ipv4.conf.all.rp_filter=0"
	err, _ = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info(cmdstr, err)
	}

	cmdstr = "iptables -w -A FORWARD -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1300"
	err, _ = public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info(cmdstr, err)
	}

	/* 检查CPE是否有Sn, 如果没有，则退出recovery */
	if public.G_coreConf.Sn == "" {
		return derr.Error{In: err.Error(), Out: "SnIsNull"}
	}

	return nil
}

func portRecover() error {

	paths := []string{config.PortConfPath}
	ports, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("ports not found: ", err.Error())
		return nil
	}

	for _, value := range ports {
		agentLog.AgentLogger.Info("recover port: " + value)

		bytes := []byte(value)

		fp := &app.PortConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]port data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]port %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover port success, id=" + fp.Id)
	}

	return nil
}

func connRecover() error {

	paths := []string{config.ConnConfPath}
	conns, err := etcdClient.GetValues(paths)
	if err != nil {
		agentLog.AgentLogger.Info("conns not found: ", err.Error())
		return nil
	}

	for _, value := range conns {
		agentLog.AgentLogger.Info("recover conn: " + value)

		bytes := []byte(value)

		fp := &app.ConnConf{}
		err := json.Unmarshal(bytes, fp)
		if err != nil {
			agentLog.AgentLogger.Info("[ERROR]conn data unmarshal failed: ", err.Error())
			continue
		}

		err = fp.Create(public.ACTION_RECOVER)
		if err != nil {
			agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]conn %s create failed: %s", fp.Id, err))
			continue
		}

		agentLog.AgentLogger.Info("recover conn success, id=" + fp.Id)
	}

	return nil
}

func haRecover() error {

	value, err := etcdClient.GetValue(config.HaConfPath)
	if err != nil {
		agentLog.AgentLogger.Info("ha not found: ", err.Error())
		return nil
	}

	agentLog.AgentLogger.Info("recover ha: " + value)
	bytes := []byte(value)
	fp := &app.HaConf{}
	err = json.Unmarshal(bytes, fp)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]ha data unmarshal failed: ", err.Error())
		return nil
	}

	err = fp.Recover()
	if err != nil {
		agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]ha recover failed: %s", err))
	}

	return nil
}

func dhcpRecover() error {

	value, err := etcdClient.GetValue(config.DhcpConfPath)
	if err != nil {
		agentLog.AgentLogger.Info("dhcp not found: ", err.Error())
		return nil
	}

	agentLog.AgentLogger.Info("recover dhcp: " + value)
	bytes := []byte(value)
	fp := &app.DhcpConf{}
	err = json.Unmarshal(bytes, fp)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]dhcp data unmarshal failed: ", err.Error())
		return nil
	}

	err = fp.Recover()
	if err != nil {
		agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]dhcp recover failed: %s", err))
	}

	return nil
}

func dnsRecover() error {

	value, err := etcdClient.GetValue(config.DnsConfPath)
	if err != nil {
		agentLog.AgentLogger.Info("dns not found: ", err.Error())
		return nil
	}

	agentLog.AgentLogger.Info("recover dns: " + value)
	bytes := []byte(value)
	fp := &app.DnsConf{}
	err = json.Unmarshal(bytes, fp)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]dns data unmarshal failed: ", err.Error())
		return nil
	}

	err = fp.Recover()
	if err != nil {
		agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]dns recover failed: %s", err))
	}

	return nil
}

func generalRecover() error {

	value, err := etcdClient.GetValue(config.GeneralConfPath)
	if err != nil {
		agentLog.AgentLogger.Info("general not found: ", err.Error())
		return nil
	}

	agentLog.AgentLogger.Info("recover general: " + value)
	bytes := []byte(value)
	fp := &app.GeneralConf{}
	err = json.Unmarshal(bytes, fp)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]general data unmarshal failed: ", err.Error())
		return nil
	}

	err = fp.Create(public.ACTION_RECOVER)
	if err != nil {
		agentLog.AgentLogger.Info(fmt.Sprintf("[ERROR]general recover failed: %s", err))
	}

	/* recover 删除文件 */
	if public.FileExists("/var/run/haLocalNat") {
		os.Remove("/var/run/haLocalNat")
	}

	if public.FileExists("/var/run/haSslconnections") {
		os.Remove("/var/run/haSslconnections")
	}

	return nil
}

func frrRecover() error {

	cmdstr := "systemctl restart frr"
	err, _ := public.ExecBashCmdWithRet(cmdstr)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]frrRecover: ", cmdstr, " err:", err)
		return err
	}

	return nil
}

func cpeRecover() error {

	/* 恢复前准备 */
	if err := proRecover(); err != nil {
		agentLog.AgentLogger.Error("proRecover fail %s \n", err)
		return err
	}

	/* 业务配置恢复 */
	if err := portRecover(); err != nil {
		agentLog.AgentLogger.Error("portRecover fail %s \n", err)
		return err
	}

	/* conn 恢复 */
	if err := connRecover(); err != nil {
		agentLog.AgentLogger.Error("connRecover fail %s \n", err)
		return err
	}

	/* dns 恢复 */
	if err := dnsRecover(); err != nil {
		agentLog.AgentLogger.Error("dnsRecover fail %s \n", err)
		return err
	}

	/* dhcp恢复 */
	if err := dhcpRecover(); err != nil {
		agentLog.AgentLogger.Error("dnsRecover fail %s \n", err)
		return err
	}

	/* ha恢复 */
	if err := haRecover(); err != nil {
		agentLog.AgentLogger.Error("haRecover fail %s \n", err)
		return err
	}

	/* general恢复 */
	if err := generalRecover(); err != nil {
		agentLog.AgentLogger.Error("generalRecover fail %s \n", err)
		return err
	}

	/* Router recover */
	if err := frrRecover(); err != nil {
		agentLog.AgentLogger.Error("frrRecover fail %s \n", err)
		return err
	}

	agentLog.AgentLogger.Info("cpe configure recover success")
	return nil
}

func main() {
	var err error

	runtime.LockOSThread()
	agentLog.Init(logName)
	network.Init()
	agentLog.AgentLogger.Info("cpe configure recover start.")

	err = mceetcd.Etcdinit()
	if err != nil {
		agentLog.AgentLogger.Error("init etcd failed: ", err.Error())
		return
	}

	etcdClient, err = etcd.NewEtcdClient(BackendNodes, "", "", "", false, "", "")
	if err != nil {
		agentLog.AgentLogger.Error("can not init etcd client: ", err.Error())
		return
	}

	if err = public.CpeConfigInit(); err != nil {
		agentLog.AgentLogger.Error("ConfigInit fail err: ", err)
		return
	}

	/* recovery 配置 */
	cpeRecover()
}
