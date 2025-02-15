package main

import (
	"context"
	"cpeos/agentLog"
	"cpeos/app"
	"cpeos/public"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gitlab.daho.tech/gdaho/log"
)

var (
	getConfInterval      = 15
	getConfIntervalCount = 480
	getConfLog           = "/var/log/cpe/getconf.log"
	frrConfPath          = "/etc/frr/frr.conf"
)

func WriteInfo(str string) {

	if public.FileExists(getConfLog) {
		err := os.Remove(getConfLog)
		if err != nil {
			agentLog.AgentLogger.Info(getConfLog, "[ERROR]remove err: ", err)
			return
		}
	}

	fd, err := log.OpenLogFile(getConfLog)
	if err != nil {
		agentLog.AgentLogger.Info("[ERROR]open getconf.log file err: ", err)
		return
	}
	defer fd.Close()

	n, err := fd.WriteString(str)
	if n != len(str) {
		agentLog.AgentLogger.Debug("write getconf.log file err: ", err)

	}
	if err != nil {
		agentLog.AgentLogger.Debug(fmt.Sprintf("write getconf.log file err,err: %v", err))
	}
}

func getAllConfInfo(ctx context.Context) error {

	tick := time.NewTicker(time.Duration(getConfInterval) * time.Second)
	defer tick.Stop()

	/* 设置url */
	url := fmt.Sprintf("/api/cpeConfig/cpes/%s/dpConfig", public.G_coreConf.Sn)
	count := 0

	for {
		select {
		case <-tick.C:
			count++
			/* 比较configVersion，如果有更新则重新获取配置 */
			if count >= getConfIntervalCount || public.G_HeartBeatInfo.ConfigVersion != public.G_HeartBeatInfo.ConfigVersionCore {
				count = 0
				respBody, err := public.GetRequestCore(public.G_coreConf.CoreAddress, public.G_coreConf.CorePort, public.G_coreConf.CoreProto, url)
				if err != nil {
					agentLog.AgentLogger.Info("[ERROR]Get core dpConfig err:", err, ", url:", url)
					///log.Warning("Get core dpConfig err:", err, bytedata, ", url:", url)
				} else {
					WriteInfo(string(respBody[:]))

					/* 转化成json */
					conf := &app.DpConfig{}
					if err := json.Unmarshal(respBody, conf); err != nil {
						agentLog.AgentLogger.Info("[ERROR]Get core dpConfig Unmarshal err:", err)
					} else {
						if conf.Success && conf.Ret == 0 {
							/* Update allconfigure */
							UpdateAllConf(&conf.Data)
						}
					}
				}

				/* check frr.conf */
				if public.FileExists(frrConfPath) {
					cmdstr := fmt.Sprintf("cat %s | grep 'table 100' | wc -l", frrConfPath)
					err, result_str := public.ExecBashCmdWithRet(cmdstr)
					if err == nil {
						defRouteNum, _ := strconv.ParseInt(strings.Replace(result_str, "\n", "", -1), 10, 64)
						if defRouteNum >= 2 {
							/* 设置异常标志 */
							public.G_HeartBeatInfo.Status = "WARNING"
						}
					}
				}
			}

		case <-ctx.Done():
			// 必须返回成功
			return nil
		}
	}
}

func initGetAllConf() error {
	// 定期从控制器获取CPE配置
	ctx, _ := context.WithCancel(context.Background())
	go func() {
		for {
			defer func() {
				if err := recover(); err != nil {
					/* 设置异常标志 */
					public.G_HeartBeatInfo.Status = "WARNING"
					agentLog.AgentLogger.Error("getAllConf monitor panic err: %v", err)
				}
			}()

			if err := getAllConfInfo(ctx); err == nil {
				return
			} else {
				agentLog.AgentLogger.Error("getAllConf monitor err :%v ", err)
			}
		}
	}()

	return nil
}
