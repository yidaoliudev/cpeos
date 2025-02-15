package main

import (
	"cpeos/agentLog"
	"cpeos/app"
	"cpeos/public"
	v "cpeos/version"
	"flag"
	"fmt"
	"net/http"
	"os"
)

const (
	LogName = "/var/log/cpe/cpe.log"
)

var (
	agentip, agentport *string
	version            *bool
	BgpMonitorInterval *int
)

func flagInit() error {
	version = flag.Bool("v", false, "print version and quit")

	agentip = flag.String("agentip", "0.0.0.0", "agent service ip")
	agentport = flag.String("agentport", "8001", "agent service port")
	BgpMonitorInterval = flag.Int("bgpMonitorInterval", 10, "bgp monitor interval")

	flag.Parse()
	if *version {
		fmt.Println(v.VERSION)
		os.Exit(0)
	}
	others := flag.Args()
	if len(others) > 0 {
		fmt.Println(fmt.Sprintf("unknown cpe command %v, (`cpe --help' for list)", others))
		os.Exit(0)
	}

	app.SetBgpPara(*BgpMonitorInterval)

	return nil
}

func main() {
	var err error

	if err = flagInit(); err != nil {
		fmt.Println("flagInit fail err: ", err)
		return
	}

	agentLog.Init(LogName)
	handlesInit()

	if err = public.CpeConfigInit(); err != nil {
		fmt.Println("CpeConfigInit fail err: ", err)
		return
	}

	/* 注意：initWanMonitor必须在cpeHearbeatReport之前。*/
	if err = initWanMonitor(); err != nil {
		agentLog.AgentLogger.Error("Init Wan Monitor err: ", err)
	} else {
		agentLog.AgentLogger.Info("InitWanMonitor ok.")
	}

	if err := cpeHearbeatReport(); err != nil {
		fmt.Println("cpeHearbeatReport fail err: ", err)
		return
	}

	if err = initGetAllConf(); err != nil {
		agentLog.AgentLogger.Error("Init GetAllConf err: ", err)
	} else {
		agentLog.AgentLogger.Info("InitGetAllConf ok.")
	}

	r := cpeAgentRouter()
	agentLog.AgentLogger.Info("start!! sn:", public.G_coreConf.Sn)
	agentLog.AgentLogger.Info(http.ListenAndServe(*agentip+":"+*agentport, r))
}
