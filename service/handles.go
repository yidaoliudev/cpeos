package main

import (
	"cpeos/agentLog"
	"cpeos/app"
	"cpeos/config"
	"cpeos/etcd"
	"cpeos/public"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"gitlab.daho.tech/gdaho/network"
)

type Res struct {
	Message string `json:"msg"`
	Code    string `json:"code"`
	Success bool   `json:"success"`
}

type Msg struct {
	res Res
	Err error `json:"err"`
}

// 常规 Handler
type PostHandler func(params map[string]string, data []byte, vapType string) *Res
type PutHandler func(params map[string]string, data []byte, vapType string) *Res
type DelHandler func(params map[string]string, vapType string) *Res
type GetHandler func(params map[string]string, vapType string) *Res

/* Port */
func CreatePort(params map[string]string, data []byte, vapType string) (result *Res) {
	agentLog.AgentLogger.Info("CreatePort:" + string(data[:]))

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreatePort Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	conf := &app.PortConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.PortConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.PortConfPath + conf.Id)
	if err == nil { // Accesspoint exists
		errPanic(EtcdHasExistError, EtcdHasExistError, errors.New(EtcdHasExistError))
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(public.ACTION_ADD); err != nil {
		errPanic("CreateError", "CreateError", err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.PortConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.PortConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModPort(params map[string]string, data []byte, vapType string) (result *Res) {

	agentLog.AgentLogger.Info("ModPort:" + string(data[:]))

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModPort Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	confNew := &app.PortConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("id  ", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.PortConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.PortConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.PortConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic("ModifyError", "ModifyError", err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.PortConfPath+confCur.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DeletePort(params map[string]string, vapType string) (result *Res) {

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("DeletePort Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete id: " + id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.PortConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.PortConfPath + id)
	agentLog.AgentLogger.Info("oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("Not Exists")
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	conf := &app.PortConf{}
	if err = public.GetDataFromEtcd(config.PortConfPath+id, conf); err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}

	if err = conf.Destroy(); err != nil {
		errPanic("DeleteError", "DeleteError", err)
	}

	err = etcd.EtcdDelValue(config.PortConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}

	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + " delete success.")
	return
}

/* Subnet */
func CreateSubnet(params map[string]string, data []byte, vapType string) (result *Res) {
	agentLog.AgentLogger.Info("CreateSubnet:" + string(data[:]))

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateSubnet Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	conf := &app.SubnetConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.SubnetConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.SubnetConfPath + conf.Id)
	if err == nil { // Accesspoint exists
		errPanic(EtcdHasExistError, EtcdHasExistError, errors.New(EtcdHasExistError))
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(); err != nil {
		errPanic("CreateError", "CreateError", err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.SubnetConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.SubnetConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModSubnet(params map[string]string, data []byte, vapType string) (result *Res) {

	agentLog.AgentLogger.Info("ModSubnet:" + string(data[:]))

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModSubnet Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	confNew := &app.SubnetConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("id  ", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.SubnetConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.SubnetConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.SubnetConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic("ModifyError", "ModifyError", err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.SubnetConfPath+confCur.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DeleteSubnet(params map[string]string, vapType string) (result *Res) {

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("DeleteSubnet Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete id: " + id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.SubnetConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.SubnetConfPath + id)
	agentLog.AgentLogger.Info("oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("Not Exists")
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	conf := &app.SubnetConf{}
	if err = public.GetDataFromEtcd(config.SubnetConfPath+id, conf); err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}

	if err = conf.Destroy(); err != nil {
		errPanic("DeleteError", "DeleteError", err)
	}

	err = etcd.EtcdDelValue(config.SubnetConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}

	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + " delete success.")
	return
}

/* Static */
func CreateStatic(params map[string]string, data []byte, vapType string) (result *Res) {
	agentLog.AgentLogger.Info("CreateStatic:" + string(data[:]))

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateStatic Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	conf := &app.StaticConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.StaticConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.StaticConfPath + conf.Id)
	if err == nil { // Accesspoint exists
		errPanic(EtcdHasExistError, EtcdHasExistError, errors.New(EtcdHasExistError))
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(); err != nil {
		errPanic("CreateError", "CreateError", err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.StaticConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.StaticConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModStatic(params map[string]string, data []byte, vapType string) (result *Res) {

	agentLog.AgentLogger.Info("ModStatic:" + string(data[:]))

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModStatic Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	confNew := &app.StaticConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("id  ", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.StaticConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.StaticConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.StaticConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic("ModifyError", "ModifyError", err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.StaticConfPath+confCur.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DeleteStatic(params map[string]string, vapType string) (result *Res) {

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("DeleteStatic Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete id: " + id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.StaticConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.StaticConfPath + id)
	agentLog.AgentLogger.Info("oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("Not Exists")
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	conf := &app.StaticConf{}
	if err = public.GetDataFromEtcd(config.StaticConfPath+id, conf); err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}

	if err = conf.Destroy(); err != nil {
		errPanic("DeleteError", "DeleteError", err)
	}

	err = etcd.EtcdDelValue(config.StaticConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}

	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + " delete success.")
	return
}

/* Check */
func CreateCheck(params map[string]string, data []byte, vapType string) (result *Res) {
	agentLog.AgentLogger.Info("CreateCheck:" + string(data[:]))

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("CreateCheck Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	conf := &app.CheckConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	/* 保护锁 */
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.CheckConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	//check etcd data exists
	_, err := etcd.EtcdGetValue(config.CheckConfPath + conf.Id)
	if err == nil { // Accesspoint exists
		errPanic(EtcdHasExistError, EtcdHasExistError, errors.New(EtcdHasExistError))
	}

	byteep, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("Create before data: " + string(byteep))

	if err := conf.Create(); err != nil {
		errPanic("CreateError", "CreateError", err)
	}

	saveData, err := json.Marshal(conf)
	if err != nil {
		errPanic(InternalError, InternalError, err)
	}
	agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
	err = etcd.EtcdSetValue(config.CheckConfPath+conf.Id, string(saveData[:]))
	if err != nil {
		errPanic(EtcdSetError, EtcdSetError, err)
	}
	defer rollbackEtcdRecord(config.CheckConfPath + conf.Id)

	result = &Res{Success: true, Message: string("success")}
	return
}

func ModCheck(params map[string]string, data []byte, vapType string) (result *Res) {

	agentLog.AgentLogger.Info("ModCheck:" + string(data[:]))

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ModCheck Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	confNew := &app.CheckConf{}
	if err := json.Unmarshal(data, confNew); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}

	agentLog.AgentLogger.Info("id  ", confNew.Id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.CheckConfPath+confNew.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()

	confdataOld, err := etcd.EtcdGetValue(config.CheckConfPath + confNew.Id)
	if err != nil {
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	confCur := &app.CheckConf{}
	if err = json.Unmarshal([]byte(confdataOld), confCur); err != nil {
		errPanic(InternalError, InternalError, err)
	}

	agentLog.AgentLogger.Info("confdataOld info: ", confdataOld)

	cfgChg := false
	err, cfgChg = confCur.Modify(confNew)
	if err != nil {
		errPanic("ModifyError", "ModifyError", err)
	}

	if cfgChg {
		saveData, err := json.Marshal(confCur)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}

		agentLog.AgentLogger.Info("etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.CheckConfPath+confCur.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
	}

	result = &Res{Success: true}
	return
}

func DeleteCheck(params map[string]string, vapType string) (result *Res) {

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("DeleteCheck Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	id, _ := getValue(params, "id")
	agentLog.AgentLogger.Info("Delete id: " + id)
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.CheckConfPath+id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer func() {
		vLock.(*sync.Mutex).Unlock()
		nsLockDelayDel(id)
	}()

	oldData, isExist, err := etcd.EtcdGetValueWithCheck(config.CheckConfPath + id)
	agentLog.AgentLogger.Info("oldData :" + oldData)
	if err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}
	if !isExist {
		agentLog.AgentLogger.Info("Not Exists")
		errPanic(EtcdNotExistError, EtcdNotExistError, err)
	}

	conf := &app.CheckConf{}
	if err = public.GetDataFromEtcd(config.CheckConfPath+id, conf); err != nil {
		errPanic(EtcdGetError, EtcdGetError, err)
	}

	if err = conf.Destroy(); err != nil {
		errPanic("DeleteError", "DeleteError", err)
	}

	err = etcd.EtcdDelValue(config.CheckConfPath + id)
	if err != nil {
		errPanic(EtcdDelError, EtcdDelError, err)
	}

	result = &Res{Success: true}
	agentLog.AgentLogger.Info(id + " delete success.")
	return
}

/* Bgp */
func ModBgp(params map[string]string, data []byte, vapType string) (result *Res) {

	agentLog.AgentLogger.Info("ModBgp:" + string(data[:]))
	result = &Res{Success: true}
	return
}

/* Dns */
func ModDns(params map[string]string, data []byte, vapType string) (result *Res) {

	agentLog.AgentLogger.Info("ModDns:" + string(data[:]))
	result = &Res{Success: true}
	return
}

/* Ha */
func ModHa(params map[string]string, data []byte, vapType string) (result *Res) {

	agentLog.AgentLogger.Info("ModHa:" + string(data[:]))
	result = &Res{Success: true}
	return
}

/* Wan */
func UpdateWan(conf *app.PortConf) (error, bool, string, string) {

	/* port 保护锁
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.PortConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()
	*/

	//check etcd data exists
	///agentLog.AgentLogger.Info("UpdateWan: ")
	chg := false
	curNexthop := ""
	curIpAddr := ""
	confOld, err := etcd.EtcdGetValue(config.PortConfPath + conf.Id)
	if err == nil {
		//Exist, need modify.
		confCur := &app.PortConf{}
		if err = json.Unmarshal([]byte(confOld), confCur); err != nil {
			///errPanic(InternalError, InternalError, err)
			agentLog.AgentLogger.Info("ModifyWan UnmarshalError ", err)
			return err, false, "", ""
		}

		curNexthop = confCur.Nexthop
		curIpAddr = confCur.IpAddr
		if confCur.IpAddr != conf.IpAddr || confCur.Nexthop != conf.Nexthop {
			confCur.IpAddr = conf.IpAddr
			confCur.Nexthop = conf.Nexthop
			chg = true
		}

		if chg {
			saveData, err := json.Marshal(confCur)
			if err != nil {
				///errPanic(InternalError, InternalError, err)
				agentLog.AgentLogger.Info("ModifyWan MarshallError ", err)
				return err, false, "", ""
			}
			agentLog.AgentLogger.Info("ModifyWan etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.PortConfPath+conf.Id, string(saveData[:]))
			if err != nil {
				///errPanic(EtcdSetError, EtcdSetError, err)
				agentLog.AgentLogger.Info("ModifyWan EtcdSetError ", err)
				return err, false, "", ""
			}
		}

		return nil, chg, curNexthop, curIpAddr
	}

	return err, false, "", ""
}

/* AllConf Update */
func UpdateSite(conf *app.SiteConf) (error, bool) {
	/* site 保护锁
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.SiteConfPath, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()
	*/

	//check etcd data exists
	///agentLog.AgentLogger.Info("UpdateSite: ")
	chg := false
	confOld, err := etcd.EtcdGetValue(config.SiteConfPath)
	if err != nil {
		//notExist, need Create.
		agentLog.AgentLogger.Info("Site notExist, create it.")
		if err := conf.Create(public.ACTION_ADD); err != nil {
			errPanic(ParamsError, ParamsError, err)
		}
		saveData, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}
		agentLog.AgentLogger.Info("CreateSite etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.SiteConfPath, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
		chg = true
		defer rollbackEtcdRecord(config.SiteConfPath)
	} else {
		//Exist, need modify.
		///agentLog.AgentLogger.Info("Site exist, need modify.")
		confCur := &app.SiteConf{}
		if err = json.Unmarshal([]byte(confOld), confCur); err != nil {
			errPanic(InternalError, InternalError, err)
		}

		err, chg = confCur.Modify(conf)
		if err != nil {
			errPanic(ModError, ModError, err)
		}

		if chg {
			saveData, err := json.Marshal(conf)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			agentLog.AgentLogger.Info("ModSite etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.SiteConfPath, string(saveData[:]))
			if err != nil {
				errPanic(EtcdSetError, EtcdSetError, err)
			}
		}
	}

	return nil, chg
}

func UpdateGeneral(conf *app.GeneralConf) (error, bool) {
	/* dns 保护锁
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.GeneralConfPath, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()
	*/

	//check etcd data exists
	///agentLog.AgentLogger.Info("UpdateDns: ")
	chg := false
	confOld, err := etcd.EtcdGetValue(config.GeneralConfPath)
	if err != nil {
		//notExist, need Create.
		agentLog.AgentLogger.Info("General notExist, create it.")
		if err := conf.Create(public.ACTION_ADD); err != nil {
			errPanic(ParamsError, ParamsError, err)
		}
		saveData, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}
		agentLog.AgentLogger.Info("CreateGeneral etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.GeneralConfPath, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
		chg = true
		defer rollbackEtcdRecord(config.GeneralConfPath)
	} else {
		//Exist, need modify.
		///agentLog.AgentLogger.Info("DNS exist, need modify.")
		confCur := &app.GeneralConf{}
		if err = json.Unmarshal([]byte(confOld), confCur); err != nil {
			errPanic(InternalError, InternalError, err)
		}

		err, chg = confCur.Modify(conf)
		if err != nil {
			errPanic(ModError, ModError, err)
		}

		if chg {
			saveData, err := json.Marshal(conf)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			agentLog.AgentLogger.Info("ModGeneral etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.GeneralConfPath, string(saveData[:]))
			if err != nil {
				errPanic(EtcdSetError, EtcdSetError, err)
			}
		}
	}

	return nil, chg
}

func UpdateDns(conf *app.DnsConf) (error, bool) {

	/* dns 保护锁
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.DnsConfPath, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()
	*/

	//check etcd data exists
	///agentLog.AgentLogger.Info("UpdateDns: ")
	chg := false
	confOld, err := etcd.EtcdGetValue(config.DnsConfPath)
	if err != nil {
		//notExist, need Create.
		agentLog.AgentLogger.Info("DNS notExist, create it.")
		if err := conf.Create(); err != nil {
			errPanic(ParamsError, ParamsError, err)
		}
		saveData, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}
		agentLog.AgentLogger.Info("CreateDns etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.DnsConfPath, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
		chg = true
		defer rollbackEtcdRecord(config.DnsConfPath)
	} else {
		//Exist, need modify.
		///agentLog.AgentLogger.Info("DNS exist, need modify.")
		confCur := &app.DnsConf{}
		if err = json.Unmarshal([]byte(confOld), confCur); err != nil {
			errPanic(InternalError, InternalError, err)
		}

		err, chg = confCur.Modify(conf)
		if err != nil {
			errPanic(ModError, ModError, err)
		}

		if chg {
			saveData, err := json.Marshal(conf)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			agentLog.AgentLogger.Info("ModDns etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.DnsConfPath, string(saveData[:]))
			if err != nil {
				errPanic(EtcdSetError, EtcdSetError, err)
			}
		}
	}

	return nil, chg
}

func UpdateDhcp(conf *app.DhcpConf) (error, bool) {

	/* Dhcp 保护锁
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.DhcpConfPath, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()
	*/

	//check etcd data exists
	///agentLog.AgentLogger.Info("UpdateDhcp: ")
	chg := false
	confOld, err := etcd.EtcdGetValue(config.DhcpConfPath)
	if err != nil {
		//notExist, need Create.
		agentLog.AgentLogger.Info("Dhcp notExist, create it")
		if err := conf.Create(); err != nil {
			errPanic(ParamsError, ParamsError, err)
		}

		saveData, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}
		agentLog.AgentLogger.Info("CreateDhcp etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.DhcpConfPath, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
		chg = true
		defer rollbackEtcdRecord(config.DhcpConfPath)
	} else {
		//Exist, need modify.
		///agentLog.AgentLogger.Info("Dhcp Exist, need modify.")
		confCur := &app.DhcpConf{}
		if err = json.Unmarshal([]byte(confOld), confCur); err != nil {
			errPanic(InternalError, InternalError, err)
		}

		err, chg = confCur.Modify(conf)
		if err != nil {
			errPanic(ModError, ModError, err)
		}

		if chg {
			saveData, err := json.Marshal(conf)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			agentLog.AgentLogger.Info("ModDhcp etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.DhcpConfPath, string(saveData[:]))
			if err != nil {
				errPanic(EtcdSetError, EtcdSetError, err)
			}
		}
	}

	return nil, chg
}

func UpdateHa(conf *app.HaConf) (error, bool) {

	/* ha 保护锁
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.HaConfPath, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()
	*/

	//check etcd data exists
	///agentLog.AgentLogger.Info("UpdateHa: ")
	chg := false
	confOld, err := etcd.EtcdGetValue(config.HaConfPath)
	if err != nil {
		//notExist, need Create.
		agentLog.AgentLogger.Info("Ha notExist, create it")
		if err := conf.Create(); err != nil {
			errPanic(ParamsError, ParamsError, err)
		}

		saveData, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}
		agentLog.AgentLogger.Info("CreateHa etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.HaConfPath, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
		chg = true
		defer rollbackEtcdRecord(config.HaConfPath)
	} else {
		//Exist, need modify.
		///agentLog.AgentLogger.Info("Ha Exist, need modify.")
		confCur := &app.HaConf{}
		if err = json.Unmarshal([]byte(confOld), confCur); err != nil {
			errPanic(InternalError, InternalError, err)
		}

		err, chg = confCur.Modify(conf)
		if err != nil {
			errPanic(ModError, ModError, err)
		}

		if chg {
			saveData, err := json.Marshal(conf)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			agentLog.AgentLogger.Info("ModHa etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.HaConfPath, string(saveData[:]))
			if err != nil {
				errPanic(EtcdSetError, EtcdSetError, err)
			}
		}
	}

	return nil, chg
}

func UpdateBgp(conf *app.BgpConf) (error, bool) {

	/* bgp 保护锁
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.BgpConfPath, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()
	*/

	//check etcd data exists
	///agentLog.AgentLogger.Info("UpdateBgp: ")
	chg := false
	confOld, err := etcd.EtcdGetValue(config.BgpConfPath)
	if err != nil {
		//notExist, need Create.
		agentLog.AgentLogger.Info("BGP notExist, create it.")
		if err := conf.Create(); err != nil {
			errPanic(ParamsError, ParamsError, err)
		}

		saveData, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}
		agentLog.AgentLogger.Info("CreateBgp etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.BgpConfPath, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
		chg = true
		defer rollbackEtcdRecord(config.BgpConfPath)
	} else {
		//Exist, need modify.
		///agentLog.AgentLogger.Info("BGP Exist, need modify.")
		confCur := &app.BgpConf{}
		if err = json.Unmarshal([]byte(confOld), confCur); err != nil {
			errPanic(InternalError, InternalError, err)
		}

		err, chg = confCur.Modify(conf)
		if err != nil {
			errPanic(ModError, ModError, err)
		}

		if chg {
			saveData, err := json.Marshal(conf)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			agentLog.AgentLogger.Info("ModBgp etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.BgpConfPath, string(saveData[:]))
			if err != nil {
				errPanic(EtcdSetError, EtcdSetError, err)
			}
		}
	}

	return nil, chg
}

func UpdatePort(conf *app.PortConf) (error, bool) {

	/* port 保护锁
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.PortConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()
	*/

	//check etcd data exists
	///agentLog.AgentLogger.Info("UpdatePort: ")
	chg := false
	confOld, err := etcd.EtcdGetValue(config.PortConfPath + conf.Id)
	if err != nil {
		//notExist, need Create.
		if err := conf.Create(public.ACTION_ADD); err != nil {
			errPanic(ParamsError, ParamsError, err)
		}

		saveData, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}
		agentLog.AgentLogger.Info("CreatePort etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.PortConfPath+conf.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
		chg = true
		defer rollbackEtcdRecord(config.PortConfPath + conf.Id)
	} else {
		//Exist, need modify.
		confCur := &app.PortConf{}
		if err = json.Unmarshal([]byte(confOld), confCur); err != nil {
			errPanic(InternalError, InternalError, err)
		}

		err, chg = confCur.Modify(conf)
		if err != nil {
			errPanic(ModError, ModError, err)
		}

		if chg {
			saveData, err := json.Marshal(confCur)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			agentLog.AgentLogger.Info("ModPort etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.PortConfPath+conf.Id, string(saveData[:]))
			if err != nil {
				errPanic(EtcdSetError, EtcdSetError, err)
			}
		}
	}

	return nil, chg
}

func UpdateSubnet(conf *app.SubnetConf) (error, bool) {

	/* subnet 保护锁
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.SubnetConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()
	*/

	//subnet etcd data exists
	///agentLog.AgentLogger.Info("UpdateSubnet: ")
	chg := false
	confOld, err := etcd.EtcdGetValue(config.SubnetConfPath + conf.Id)
	if err != nil {
		//notExist, need Create.
		if err := conf.Create(); err != nil {
			errPanic(ParamsError, ParamsError, err)
		}

		saveData, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}
		agentLog.AgentLogger.Info("CreateSubnet etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.SubnetConfPath+conf.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
		chg = true
		defer rollbackEtcdRecord(config.SubnetConfPath + conf.Id)
	} else {
		//Exist, need modify.
		confCur := &app.SubnetConf{}
		if err = json.Unmarshal([]byte(confOld), confCur); err != nil {
			errPanic(InternalError, InternalError, err)
		}

		err, chg = confCur.Modify(conf)
		if err != nil {
			errPanic(ModError, ModError, err)
		}

		if chg {
			saveData, err := json.Marshal(confCur)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			agentLog.AgentLogger.Info("ModSubnet etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.SubnetConfPath+conf.Id, string(saveData[:]))
			if err != nil {
				errPanic(EtcdSetError, EtcdSetError, err)
			}
		}
	}

	return nil, chg
}

func UpdateConn(conf *app.ConnConf) (error, bool) {

	/* conn 保护锁
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.ConnConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()
	*/
	//check etcd data exists
	///agentLog.AgentLogger.Info("UpdateConn: ")
	chg := false
	confOld, err := etcd.EtcdGetValue(config.ConnConfPath + conf.Id)
	if err != nil {
		//notExist, need Create.
		if err := conf.Create(public.ACTION_ADD); err != nil {
			errPanic(ParamsError, ParamsError, err)
		}

		saveData, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}
		agentLog.AgentLogger.Info("CreateConn etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.ConnConfPath+conf.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
		chg = true
		defer rollbackEtcdRecord(config.ConnConfPath + conf.Id)
	} else {
		//Exist, need modify.
		confCur := &app.ConnConf{}
		if err = json.Unmarshal([]byte(confOld), confCur); err != nil {
			errPanic(InternalError, InternalError, err)
		}

		if confCur.Type != conf.Type {
			err = confCur.Destroy()
			if err != nil {
				errPanic(ModError, ModError, err)
			}
			err = conf.Create(public.ACTION_ADD)
			if err != nil {
				errPanic(ModError, ModError, err)
			}

			saveData, err := json.Marshal(conf)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			agentLog.AgentLogger.Info("ModConn etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.ConnConfPath+conf.Id, string(saveData[:]))
			if err != nil {
				errPanic(EtcdSetError, EtcdSetError, err)
			}
			chg = true
			defer rollbackEtcdRecord(config.ConnConfPath + conf.Id)
		} else {
			err, chg = confCur.Modify(conf)
			if err != nil {
				errPanic(ModError, ModError, err)
			}

			if chg {
				saveData, err := json.Marshal(confCur)
				if err != nil {
					errPanic(InternalError, InternalError, err)
				}
				agentLog.AgentLogger.Info("ModConn etcd save data: " + string(saveData[:]))
				err = etcd.EtcdSetValue(config.ConnConfPath+conf.Id, string(saveData[:]))
				if err != nil {
					errPanic(EtcdSetError, EtcdSetError, err)
				}
			}
		}
	}

	return nil, chg
}

func UpdateStatic(conf *app.StaticConf) (error, bool) {

	/* static 保护锁
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.StaticConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()
	*/
	//check etcd data exists
	chg := false
	///agentLog.AgentLogger.Info("UpdateStatic: ")
	confOld, err := etcd.EtcdGetValue(config.StaticConfPath + conf.Id)
	if err != nil {
		//notExist, need Create.
		if err := conf.Create(); err != nil {
			errPanic(ParamsError, ParamsError, err)
		}

		saveData, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}
		agentLog.AgentLogger.Info("CreateStatic etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.StaticConfPath+conf.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
		chg = true
		defer rollbackEtcdRecord(config.StaticConfPath + conf.Id)
	} else {
		//Exist, need modify.
		confCur := &app.StaticConf{}
		if err = json.Unmarshal([]byte(confOld), confCur); err != nil {
			errPanic(InternalError, InternalError, err)
		}

		err, chg = confCur.Modify(conf)
		if err != nil {
			errPanic(ModError, ModError, err)
		}

		if chg {
			saveData, err := json.Marshal(confCur)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			agentLog.AgentLogger.Info("ModStatic etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.StaticConfPath+conf.Id, string(saveData[:]))
			if err != nil {
				errPanic(EtcdSetError, EtcdSetError, err)
			}
		}
	}

	return nil, chg
}

func UpdateCheck(conf *app.CheckConf) (error, bool) {

	/* static 保护锁
	vLock, _ := public.EtcdLock.NsLock.LoadOrStore(config.CheckConfPath+conf.Id, new(sync.Mutex))
	vLock.(*sync.Mutex).Lock()
	defer vLock.(*sync.Mutex).Unlock()
	*/
	//check etcd data exists
	///agentLog.AgentLogger.Info("UpdateCheck: ")
	chg := false
	confOld, err := etcd.EtcdGetValue(config.CheckConfPath + conf.Id)
	if err != nil {
		//notExist, need Create.
		if err := conf.Create(); err != nil {
			errPanic(ParamsError, ParamsError, err)
		}

		saveData, err := json.Marshal(conf)
		if err != nil {
			errPanic(InternalError, InternalError, err)
		}
		agentLog.AgentLogger.Info("CreateCheck etcd save data: " + string(saveData[:]))
		err = etcd.EtcdSetValue(config.CheckConfPath+conf.Id, string(saveData[:]))
		if err != nil {
			errPanic(EtcdSetError, EtcdSetError, err)
		}
		chg = true
		defer rollbackEtcdRecord(config.CheckConfPath + conf.Id)
	} else {
		//Exist, need modify.
		confCur := &app.CheckConf{}
		if err = json.Unmarshal([]byte(confOld), confCur); err != nil {
			errPanic(InternalError, InternalError, err)
		}

		err, chg = confCur.Modify(conf)
		if err != nil {
			errPanic(ModError, ModError, err)
		}

		if chg {
			saveData, err := json.Marshal(confCur)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			agentLog.AgentLogger.Info("ModCheck etcd save data: " + string(saveData[:]))
			err = etcd.EtcdSetValue(config.CheckConfPath+conf.Id, string(saveData[:]))
			if err != nil {
				errPanic(EtcdSetError, EtcdSetError, err)
			}
		}
	}

	return nil, chg
}

func RemovePort(arry []app.PortConf) bool {
	var found bool
	var err error
	chg := false
	paths := []string{config.PortConfPath}
	ports, err := etcd.EtcdGetValues(paths)
	if err == nil {
		for _, value := range ports {
			bytes := []byte(value)
			fp := &app.PortConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			found = false
			for _, port := range arry {
				if port.Id == fp.Id {
					found = true
					break
				}
			}

			if !found {
				agentLog.AgentLogger.Info("RemovePort Id: ", fp.Id)
				err = fp.Destroy()
				if err != nil {
					errPanic(DestroyError, DestroyError, err)
				}
				err = etcd.EtcdDelValue(config.PortConfPath + fp.Id)
				if err != nil {
					errPanic(EtcdDelError, EtcdDelError, err)
				}
				chg = true
			}
		}
	}

	return chg
}

func RemoveConn(arry []app.ConnConf) bool {
	var found bool
	var err error
	chg := false
	paths := []string{config.ConnConfPath}
	conns, err := etcd.EtcdGetValues(paths)
	if err == nil {
		for _, value := range conns {
			bytes := []byte(value)
			fp := &app.ConnConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			found = false
			for _, conn := range arry {
				if conn.Id == fp.Id {
					found = true
					break
				}
			}

			if !found {
				agentLog.AgentLogger.Info("RemoveConn Id: ", fp.Id)
				err = fp.Destroy()
				if err != nil {
					errPanic(DestroyError, DestroyError, err)
				}
				err = etcd.EtcdDelValue(config.ConnConfPath + fp.Id)
				if err != nil {
					errPanic(EtcdDelError, EtcdDelError, err)
				}
				chg = true
			}
		}
	}

	return chg
}

func RemoveStatic(arry []app.StaticConf) bool {
	var found bool
	var err error
	chg := false
	paths := []string{config.StaticConfPath}
	statics, err := etcd.EtcdGetValues(paths)
	if err == nil {
		for _, value := range statics {
			bytes := []byte(value)
			fp := &app.StaticConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			found = false
			for _, static := range arry {
				if static.Id == fp.Id {
					found = true
					break
				}
			}

			if !found {
				agentLog.AgentLogger.Info("RemoveStatic Id: ", fp.Id)
				err = fp.Destroy()
				if err != nil {
					errPanic(DestroyError, DestroyError, err)
				}
				err = etcd.EtcdDelValue(config.StaticConfPath + fp.Id)
				if err != nil {
					errPanic(EtcdDelError, EtcdDelError, err)
				}
				chg = true
			}
		}
	}

	return chg
}

func RemoveSubnet(arry []app.SubnetConf) bool {
	var found bool
	var err error
	chg := false
	paths := []string{config.SubnetConfPath}
	subnets, err := etcd.EtcdGetValues(paths)
	if err == nil {
		for _, value := range subnets {
			bytes := []byte(value)
			fp := &app.SubnetConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			found = false
			for _, subnet := range arry {
				if subnet.Id == fp.Id {
					found = true
					break
				}
			}

			if !found {
				agentLog.AgentLogger.Info("RemoveSubnet Id: ", fp.Id)
				err = fp.Destroy()
				if err != nil {
					errPanic(DestroyError, DestroyError, err)
				}
				err = etcd.EtcdDelValue(config.SubnetConfPath + fp.Id)
				if err != nil {
					errPanic(EtcdDelError, EtcdDelError, err)
				}
				chg = true
			}
		}
	}

	return chg
}

func RemoveCheck(arry []app.CheckConf) bool {
	var found bool
	var err error
	chg := false
	paths := []string{config.CheckConfPath}
	checks, err := etcd.EtcdGetValues(paths)
	if err == nil {
		for _, value := range checks {
			bytes := []byte(value)
			fp := &app.CheckConf{}
			err := json.Unmarshal(bytes, fp)
			if err != nil {
				continue
			}

			found = false
			for _, check := range arry {
				if check.Id == fp.Id {
					found = true
					break
				}
			}

			if !found {
				agentLog.AgentLogger.Info("RemoveCheck Id: ", fp.Id)
				err = fp.Destroy()
				if err != nil {
					errPanic(DestroyError, DestroyError, err)
				}
				err = etcd.EtcdDelValue(config.CheckConfPath + fp.Id)
				if err != nil {
					errPanic(EtcdDelError, EtcdDelError, err)
				}
				chg = true
			}
		}
	}

	return chg
}

func UpdateAllConf(allConf *app.AllConf) bool {

	chg := false

	/*1. Sync configure，modify or cretate. */
	for _, port := range allConf.PortConfig {
		if _, cfgchg := UpdatePort(&port); cfgchg {
			chg = true
		}
	}

	for _, subnet := range allConf.SubnetConfig {
		if _, cfgchg := UpdateSubnet(&subnet); cfgchg {
			chg = true
		}
	}

	for _, conn := range allConf.ConnConfig {
		if _, cfgchg := UpdateConn(&conn); cfgchg {
			chg = true
		}
	}

	for _, static := range allConf.StaticConfig {
		if _, cfgchg := UpdateStatic(&static); cfgchg {
			chg = true
		}
	}

	for _, check := range allConf.CheckConfig {
		if _, cfgchg := UpdateCheck(&check); cfgchg {
			chg = true
		}
	}
	/*2. Delete old configre. */
	if RemoveCheck(allConf.CheckConfig) {
		chg = true
	}
	if RemoveStatic(allConf.StaticConfig) {
		chg = true
	}
	if RemoveConn(allConf.ConnConfig) {
		chg = true
	}
	if RemoveSubnet(allConf.SubnetConfig) {
		chg = true
	}

	/*3. Update services. */
	if _, cfgchg := UpdateBgp(&allConf.BgpConfig); cfgchg {
		chg = true
	}
	if _, cfgchg := UpdateDns(&allConf.DnsConfig); cfgchg {
		chg = true
	}
	if _, cfgchg := UpdateDhcp(&allConf.DhcpConfig); cfgchg {
		chg = true
	}
	if _, cfgchg := UpdateHa(&allConf.HaConfig); cfgchg {
		chg = true
	}
	if _, cfgchg := UpdateGeneral(&allConf.GeneralConfig); cfgchg {
		chg = true
	}
	if _, cfgchg := UpdateSite(&allConf.SiteConfig); cfgchg {
		chg = true
	}

	/* 4. Delete old port. */
	if RemovePort(allConf.PortConfig) {
		chg = true
	}

	if chg {
		data, _ := json.Marshal(allConf)
		agentLog.AgentLogger.Info("UpdateAllConf data: " + string(data[:]))
	}

	return chg
}

func ConfigAll(params map[string]string, data []byte, vapType string) (result *Res) {

	var err error
	agentLog.AgentLogger.Info("CPE ConfigAll :" + string(data[:]))

	/* check sn */
	sn, _ := getValue(params, "sn")
	agentLog.AgentLogger.Info("ConfigAll Get SN " + sn)

	if !public.CheckSn(sn) {
		result = &Res{Success: false, Code: "500", Message: CheckSnError}
		return
	}

	allConf := &app.AllConf{}
	if err = json.Unmarshal(data, allConf); err != nil {
		errPanic(ParamsError, ParamsError, err)
	}
	UpdateAllConf(allConf)

	result = &Res{Success: true, Message: string("success")}
	return
}

func PostDecorator(handler PostHandler, vapType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checkContentType(w, r) {
			res, err := ioutil.ReadAll(r.Body)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			vars := mux.Vars(r)
			result := handler(vars, res, vapType)
			sendJson(result, w, r)
		} else {
			errPanic(ContentTypeError, ContentTypeError, nil)
		}
	}
}

func PutDecorator(handler PutHandler, vapType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checkContentType(w, r) {
			res, err := ioutil.ReadAll(r.Body)
			if err != nil {
				errPanic(InternalError, InternalError, err)
			}
			vars := mux.Vars(r)
			result := handler(vars, res, vapType)
			sendJson(result, w, r)
		}
	}
}

func DelDecorator(hander DelHandler, vapType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		result := hander(vars, vapType)
		sendJson(result, w, r)
	}
}

func GetDecorator(hander GetHandler, vapType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		result := hander(vars, vapType)
		sendJson(result, w, r)
	}
}

func sendJson(m *Res, w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(m); err != nil {
		panic(err)
	}
	return true
}

func checkContentType(w http.ResponseWriter, r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Content-Type"), "application/json; charset=utf-8") && !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		result := Res{Success: false, Code: "Error: Content-type is error", Message: "Content-type : application/json"}
		agentLog.HttpLog("info", r, "Content-Type Error : "+r.Header.Get("Content-Type"))
		sendJson(&result, w, r)
		return false
	}
	return true
}

func handlesInit() {

	network.Init()

	err := etcd.Etcdinit()
	if err != nil {
		agentLog.AgentLogger.Info("Etcdinit.", err)
		os.Exit(1)
	} else {
		agentLog.AgentLogger.Info("init etcd client success.")
	}

	/*
		//TODO
		if err = app.InitBgpMonitor(); err != nil {
			agentLog.AgentLogger.Error("Init Bgp Monitor err: ", err)
		} else {
			agentLog.AgentLogger.Info("InitBgpMonitor ok.")
		}
	*/
}
