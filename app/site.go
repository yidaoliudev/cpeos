package app

import (
	"bufio"
	"cpeos/agentLog"
	"cpeos/public"
	"net"
	"os"
	"strings"

	"gitlab.daho.tech/gdaho/util/derr"
)

const (
	CoreListPath = "/mnt/agent/coreList.conf"
)

type SiteConf struct {
	Id            string   `json:"id"`
	CoreList      []string `json:"coreList"`
	ConfigVersion int      `json:"configVersion"`
}

func WriteCoreListConf(coreList []string) error {

	// 打开文件，清空内容
	file, err := os.OpenFile(CoreListPath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0666)
	if err != nil {
		agentLog.AgentLogger.Error("OpenFile coreList err: ", err)
		return err
	}
	defer file.Close()

	// 准备写入数据
	writer := bufio.NewWriter(file)

	// 写入每个字符串
	for _, line := range coreList {
		ipAddr := line
		if strings.Contains(line, "/") {
			ipAddr = strings.Split(line, "/")[0]
		}
		_, err := writer.WriteString(ipAddr + "\n")
		if err != nil {
			agentLog.AgentLogger.Error("WriteString coreList err: ", err)
			return err
		}
	}

	// 刷新缓冲区，确保所有数据都被写入
	err = writer.Flush()
	if err != nil {
		agentLog.AgentLogger.Error("Flush coreList err: ", err)
		return err
	}

	return nil
}

func updateCoreList(coreList []string) error {

	var err error

	/* 检查数据源格式，是否为IPv4地址 */
	for _, value := range coreList {
		ipAddr := value
		if strings.Contains(value, "/") {
			ipAddr = strings.Split(value, "/")[0]
		}

		if net.ParseIP(ipAddr).To4() == nil {
			agentLog.AgentLogger.Info(err, "Update coreList fail, address not IPv4: ", value)
			return derr.Error{In: err.Error(), Out: "AddressNotIpv4"}
		}
	}

	/* 更新coreList文件 */
	return WriteCoreListConf(coreList)
}

func (conf *SiteConf) Create(action int) error {
	if conf.Id != "" {
		public.G_HeartBeatInfo.ConfigVersion = conf.ConfigVersion
	} else {
		public.G_HeartBeatInfo.ConfigVersion = 0
	}

	updateCoreList(conf.CoreList)
	return nil
}

func (cfgCur *SiteConf) Modify(cfgNew *SiteConf) (error, bool) {
	chg := false
	if cfgCur.Id != cfgNew.Id {
		chg = true
	}

	if cfgCur.ConfigVersion != cfgNew.ConfigVersion {
		chg = true
	}

	if cfgNew.Id != "" {
		public.G_HeartBeatInfo.ConfigVersion = cfgNew.ConfigVersion
	} else if cfgNew.Id == "" {
		public.G_HeartBeatInfo.ConfigVersion = 0
	}

	add, delete := public.Arrcmp(cfgCur.CoreList, cfgNew.CoreList)
	if len(add) != 0 || len(delete) != 0 {
		updateCoreList(cfgNew.CoreList)
		chg = true
	}

	return nil, chg
}
