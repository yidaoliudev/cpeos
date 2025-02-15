package main

import (
	"cpeos/agentLog"
	"cpeos/etcd"
	"cpeos/public"
	"errors"
	"time"
)

func rollbackNs(NamespaceId string) {
	if err := recover(); err != nil {
		///DestroyAneAll(NamespaceId)
		panic(err)
	}
}

func rollbackEtcdRecord(path string) {
	if err := recover(); err != nil {
		err1 := etcd.EtcdDelValue(path)
		if err1 != nil {
			agentLog.AgentLogger.Error(err1.Error())
			agentLog.AgentLogger.Error("RollBack etcd record failed!!!")
		}
		panic(err)
	}
}

func errPanic(code, message string, err error) {
	msg := new(Msg)
	msg.res.Success = false
	msg.res.Code = "500"
	msg.res.Message = message
	msg.Err = err
	panic(*msg)
}

func getValue(m map[string]string, key string) (string, error) {
	if m == nil {
		return "", errors.New(ParamsError)
	}
	if val, ok := m[key]; ok {
		return val, nil
	} else {
		return "", errors.New(ParamsError)
	}
}

// 60秒后删除
func nsLockDelayDel(key string) {
	go func() {
		<-time.After(60 * time.Second)

		public.EtcdLock.NsLock.Delete(key)
		agentLog.AgentLogger.Info("delete lock for %v", key)
	}()
}
