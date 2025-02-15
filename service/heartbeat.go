package main

import (
	"context"
	"cpeos/agentLog"
	"cpeos/public"
	v "cpeos/version"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"
)

type httpInfo struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Msg     string `json:"msg"`
}

type HeartBeatReq struct {
	Version       string `json:"version"`
	Status        string `json:"status"`
	ConfigVersion int    `json:"configVersion"`
}

type HeartbeatRespDate struct {
	ConfigVersion int `json:"configVersion"`
}

type HeartbeatResp struct {
	Success bool              `json:"success"`
	Ret     int               `json:"ret"`
	Code    string            `json:"code"`
	Msg     string            `json:"msg"`
	Data    HeartbeatRespDate `json:"data"`
}

var (
	heartBeatInterval = 10
)

func cpeHearbeatReport() error {

	var buf [2048]byte

	cmdstr := fmt.Sprintf("echo $(date '+%Y-%m-%d %H:%M:%S')")
	err, times := public.ExecBashCmdWithRet(cmdstr)
	if err != nil || times == "" {
		agentLog.AgentLogger.Error(cmdstr, err, "HeartBeat get time fail: ", times)
		os.Exit(0)
		return err
	}

	/* 第一次上报心跳失败了程序就退出 */
	cmdstr = fmt.Sprintf("/usr/bin/curl  -X PUT -H 'X-Request-Source: admin-api' -d  '{\"version\":\"%s\",\"configVersion\":0,\"status\":\"NORMAL\"}' -L -k -s %s://%s:%d/api/cpeConfig/cpes/%s/heartbeat",
		v.VERSION, public.G_coreConf.CoreProto, public.G_coreConf.CoreAddress, public.G_coreConf.CorePort, public.G_coreConf.Sn)
	err, res_str := public.ExecBashCmdWithRet(cmdstr)
	agentLog.AgentLogger.Info("cmd_str:", cmdstr, " res_str", res_str, " err", err)
	if err != nil {
		agentLog.AgentLogger.Info("HeartBeat curl err :", err)
		time.Sleep(60 * time.Second)
		os.Exit(0)
		return err
	} else {
		fp := &httpInfo{}
		if err := json.Unmarshal([]byte(res_str), fp); err != nil {
			agentLog.AgentLogger.Info("HeartBeat httpInfo err :", err)
			return err
		}
		if !fp.Success {
			agentLog.AgentLogger.Info("HeartBeat cmd_str:", cmdstr, "err:", err, "res_str:", res_str)
			fmt.Println(fp.Code, fp.Msg)
			os.Exit(0)
			return err
		}

	}

	public.G_HeartBeatInfo = public.HeartBeatInfo{Version: v.VERSION, Status: "NORMAL", ConfigVersion: -1, ConfigVersionCore: -1}
	url := fmt.Sprintf("/api/cpeConfig/cpes/%s/heartbeat", public.G_coreConf.Sn)
	ctx, _ := context.WithCancel(context.Background())
	agentLog.AgentLogger.Info("HeartBeat init ok, url", url, " G_HeartBeatInfo: ", public.G_HeartBeatInfo)
	go func() {
		for {
			defer func() {
				agentLog.AgentLogger.Info("HeartBeat defer heatBeatReport success")
				n := runtime.Stack(buf[:], true)
				agentLog.AgentLogger.Info(string(buf[:]), n)
				if err := recover(); err != nil {
					agentLog.AgentLogger.Error("HeartBeat heartbeat err:", err)
				}
			}()

			if err := heatBeatReport(ctx, url); err == nil {
				agentLog.AgentLogger.Info("HeartBeat heatBeatReport success")
				return
			} else {
				agentLog.AgentLogger.Error("HeartBeat heatBeatReport err:", err)
			}
		}
	}()

	return nil
}

func heatBeatReport(ctx context.Context, url string) error {

	tick := time.NewTicker(time.Duration(heartBeatInterval) * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			/* 每次heartbeat使用当前最新status */
			hbReq := HeartBeatReq{public.G_HeartBeatInfo.Version, public.G_HeartBeatInfo.Status, public.G_HeartBeatInfo.ConfigVersion}
			bytedata, err := json.Marshal(hbReq)
			if err == nil {
				respBody, err := public.RequestCore(bytedata, public.G_coreConf.CoreAddress, public.G_coreConf.CorePort, public.G_coreConf.CoreProto, url)
				if err != nil {
					agentLog.AgentLogger.Info("[ERROR]HeartBeat RequestCore err : ", err)
				} else {
					/* 转化成json */
					conf := &HeartbeatResp{}
					if err := json.Unmarshal(respBody, conf); err != nil {
						agentLog.AgentLogger.Info("[ERROR]Get HeartbeatResp Unmarshal err:", err)
					} else {
						if conf.Success && conf.Ret == 0 {
							/* Update ConfigVersionCore */
							public.G_HeartBeatInfo.ConfigVersionCore = conf.Data.ConfigVersion
						}
					}
				}
			} else {
				agentLog.AgentLogger.Info("[ERROR]HeartBeat Marshal err : ", err)
			}

		case <-ctx.Done():
			agentLog.AgentLogger.Info("<-ctx.Done()")
			// 必须返回成功
			return nil
		}
	}
}
