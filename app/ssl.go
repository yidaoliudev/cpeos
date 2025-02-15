package app

import (
	"cpeos/agentLog"
	"cpeos/public"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"gitlab.daho.tech/gdaho/util/derr"
)

type SslConf struct {
	Name          string   `json:"name"`          //[继承]sslvpn名称
	Bandwidth     int      `json:"bandwidth"`     //[继承]带宽限速，单位Mbps
	TunnelDst     string   `json:"tunnelDst"`     //(必填)服务地址
	LocalAddress  string   `json:"localAddress"`  //(必填)sslvpn隧道本端地址
	RemoteAddress string   `json:"remoteAddress"` //(必填)sslvpn隧道对端地址
	HealthCheck   bool     `json:"healthCheck"`   //(必填)健康检查开关，开启时，RemoteAddress必须填写
	Nat           bool     `json:"nat"`           //[继承]SNAT开关
	NoNatCidr     []string `json:"noNatCidr"`     //[继承]
	Port          int      `json:"port"`          //(必填)服务端口
	Protocol      string   `json:"protocol"`      //(必填)服务协议
	Username      string   `json:"username"`      //(必填)账号名称
	Passwd        string   `json:"passwd"`        //(必填)账号密码
	Proto         string   `json:"proto"`         //{不填}openVPN配置文件参数
}

const (
	sslDefaultConfPath = "/home/sslclient/ns-1"
	sslNsPath          = "/home/sslclient/%s"
	clientEnvPath      = "/home/sslclient/%s_%s.env"
	clientConfPath     = "/home/sslclient/%s/%s_default.ovpn"
	clientPwsConfPath  = "/home/sslclient/%s/passwd.txt"
	clientCaConfPath   = "/home/sslclient/%s/ca.crt"
	stopSslclient      = "stop sslclient@%s_%s"
	enableSslclient    = "enable sslclient@%s_%s"
	disableSslclient   = "disable sslclient@%s_%s"
	restartSslclient   = "restart sslclient@%s_%s"
	clientNameDef      = "default"
)

const (
	SSLClientConfFormat = `client
resolv-retry infinite
verb 3
remote {{.TunnelDst}} {{.Port}}
proto {{.Proto}} 
dev {{.Name}}
dev-type tun
auth-nocache
nobind
persist-key
persist-tun
cipher AES-128-CBC
tun-mtu 1392
tls-exit
#comp-lzo no
auth-user-pass passwd.txt
ca ca.crt
`
	PswfileFormat = `{{.Username}}
{{.Passwd}}
`
	CafileFormat = `-----BEGIN CERTIFICATE-----
MIIEujCCA6KgAwIBAgIJAIdesGli9zUkMA0GCSqGSIb3DQEBCwUAMIGZMQswCQYD
VQQGEwJDTjELMAkGA1UECBMCQ0ExCzAJBgNVBAcTAlNaMQ8wDQYDVQQKEwZUSUNP
TU0xHTAbBgNVBAsTFE15T3JnYW5pemF0aW9uYWxVbml0MQswCQYDVQQDEwJjYTEQ
MA4GA1UEKRMHRWFzeVJTQTEhMB8GCSqGSIb3DQEJARYSbWVAbXlob3N0Lm15ZG9t
YWluMB4XDTE5MDMwODA3MTg1OFoXDTI5MDMwNTA3MTg1OFowgZkxCzAJBgNVBAYT
AkNOMQswCQYDVQQIEwJDQTELMAkGA1UEBxMCU1oxDzANBgNVBAoTBlRJQ09NTTEd
MBsGA1UECxMUTXlPcmdhbml6YXRpb25hbFVuaXQxCzAJBgNVBAMTAmNhMRAwDgYD
VQQpEwdFYXN5UlNBMSEwHwYJKoZIhvcNAQkBFhJtZUBteWhvc3QubXlkb21haW4w
ggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDS+lvD4Sxbbj2L/9SzP1gT
V68enlgoLTeJER+H/tnDdLjYRg0SaCa5TNQfERuQi0iT5JGSkNEyvY2iwkInBaJm
CXODEB60QjW2ocQJV9oxBZufek9boTBRQl8Do//TvLbWxL1cbEXkUsYUWujMW07F
PBYh0Oz7R0biqZjXvUQ3yWr3Si8Ir4JY7geXimAMkoiATUNT2k+SN6/U/k83/PaR
Tz6Ov2qPMuh2ze7xsUy1b2HATZpEtOmJPsZFLJ5bbiN9GGx64ApGIpl2y/pmRj7F
OsNN2LI+W72RSyaOlFUJr5LcuQoZLQqEXJwk1c9PtpNut/3iUX8Od7Bkq3YIQHGx
AgMBAAGjggEBMIH+MB0GA1UdDgQWBBRAqIWFQUrBJmLeuwK/BSVnGyqzBDCBzgYD
VR0jBIHGMIHDgBRAqIWFQUrBJmLeuwK/BSVnGyqzBKGBn6SBnDCBmTELMAkGA1UE
BhMCQ04xCzAJBgNVBAgTAkNBMQswCQYDVQQHEwJTWjEPMA0GA1UEChMGVElDT01N
MR0wGwYDVQQLExRNeU9yZ2FuaXphdGlvbmFsVW5pdDELMAkGA1UEAxMCY2ExEDAO
BgNVBCkTB0Vhc3lSU0ExITAfBgkqhkiG9w0BCQEWEm1lQG15aG9zdC5teWRvbWFp
boIJAIdesGli9zUkMAwGA1UdEwQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAL/7
BB8alP8pxM6lx+/4Z56egPKXEJmfZAA9LnZRosG+vCw2IsEDiqCuh/ZnUmpJjlh2
GkMfmsMVrNLIloqRXeRKKX1iFIlFKIR8we1uq0lDpS7sSNAAFUJzYF8Gh7kTH9Bm
y+V8YiIBxzxURuKp6k2diw7RDdOTrqk2CgER8R55dYx0AznDfrnMwhMd5a9tH5+6
SxVZYTiVfT3Hm3YhZbVQHgqeqyOaaYkzniBBVLOw/JOv2fOOD30SbtG7jbaXhQIb
iqjxj6xo3g9aHjlTmTcifL9hR9SKqyJmQx/aYqnOijvP0+LMR4Isay44muUe9vsh
GJtOpdwqtiojjIyFRAY=
-----END CERTIFICATE-----
`
)

func sslGetClientConfPath(connName string) string {
	return fmt.Sprintf(clientConfPath, connName, connName)
}

func sslGetPwsConfPath(connName string) string {
	return fmt.Sprintf(clientPwsConfPath, connName)
}

func sslGetCaConfPath(connName string) string {
	return fmt.Sprintf(clientCaConfPath, connName)
}

func sslclientStop(connName string, serverName string) error {
	para := strings.Split(fmt.Sprintf(stopSslclient, connName, serverName), " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}

	para = strings.Split(fmt.Sprintf(disableSslclient, connName, serverName), " ")
	if err := public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}
	return nil
}

func sslclientRestart(connName string) error {
	var err error
	para := strings.Split(fmt.Sprintf(enableSslclient, connName, clientNameDef), " ")
	if err = public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}

	para = strings.Split(fmt.Sprintf(restartSslclient, connName, clientNameDef), " ")
	if err = public.ExecCmd(sysctl_cmd, para...); err != nil {
		return err
	}

	return nil
}

func resetSSL(connName string, bandwidth int) error {

	var err error

	/* 服务端配置变更重启 sslclient */
	if err = sslclientRestart(connName); err != nil {
		agentLog.AgentLogger.Info("sslclientRestart  failed", connName, err)
		return err
	}

	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		if public.DeviceExist(connName) {
			break
		}
	}

	/* 重启进程，限速需要重新配置 */
	err = public.SetInterfaceIngressLimit(connName, bandwidth)
	if err != nil {
		agentLog.AgentLogger.Info("SetInterfaceIngressLimit failed: ", connName, err)
		return err
	}

	return nil
}

func ClientConfCreate(fp *SslConf) (string, error) {

	var tmpl *template.Template
	var err error

	confFile := sslGetClientConfPath(fp.Name)
	tmpl, err = template.New(fp.Name).Parse(SSLClientConfFormat)
	if err != nil {
		return confFile, err
	}

	if public.FileExists(confFile) {
		err := os.Remove(confFile)
		if err != nil {
			return confFile, err
		}
	}

	fileConf, err := os.OpenFile(confFile, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
	if err != nil && !os.IsExist(err) {
		return confFile, err
	}
	defer fileConf.Close()

	if err = tmpl.Execute(fileConf, fp); err != nil {
		if public.FileExists(confFile) {
			os.Remove(confFile)
		}

		return confFile, err
	}

	return confFile, nil
}

func PwsConfCreate(fp *SslConf) (string, error) {

	var tmpl *template.Template
	var err error

	confFile := sslGetPwsConfPath(fp.Name)
	tmpl, err = template.New(fp.Name).Parse(PswfileFormat)
	if err != nil {
		return confFile, err
	}

	if public.FileExists(confFile) {
		err := os.Remove(confFile)
		if err != nil {
			return confFile, err
		}
	}

	fileConf, err := os.OpenFile(confFile, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
	if err != nil && !os.IsExist(err) {
		return confFile, err
	}
	defer fileConf.Close()

	if err = tmpl.Execute(fileConf, fp); err != nil {
		if public.FileExists(confFile) {
			os.Remove(confFile)
		}

		return confFile, err
	}

	return confFile, nil
}

func CaConfCreate(fp *SslConf) (string, error) {

	var tmpl *template.Template
	var err error

	confFile := sslGetCaConfPath(fp.Name)
	tmpl, err = template.New(fp.Name).Parse(CafileFormat)
	if err != nil {
		return confFile, err
	}

	if public.FileExists(confFile) {
		err := os.Remove(confFile)
		if err != nil {
			return confFile, err
		}
	}

	fileConf, err := os.OpenFile(confFile, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
	if err != nil && !os.IsExist(err) {
		return confFile, err
	}
	defer fileConf.Close()

	if err = tmpl.Execute(fileConf, fp); err != nil {
		if public.FileExists(confFile) {
			os.Remove(confFile)
		}

		return confFile, err
	}

	return confFile, nil
}

func creatEnvFile(connName string, serverName string) error {
	var err error
	b := []byte("nsId=" + connName)
	EnvPath := fmt.Sprintf(clientEnvPath, connName, serverName)

	defer func() {
		if err != nil {
			if public.FileExists(EnvPath) {
				os.Remove(EnvPath)
			}
		}
	}()

	if err = public.Write2File(EnvPath, b); err != nil {
		agentLog.AgentLogger.Info("creatEnvFile failed")
		return err
	}
	return nil
}

func (conf *SslConf) Create(action int) error {
	var err error

	if action == public.ACTION_ADD {
		//文件夹存在 先删除
		dirPath := fmt.Sprintf(sslNsPath, conf.Name)
		if public.FileExists(dirPath) {
			os.RemoveAll(dirPath)
		}

		/* 创建sslclient工作目录 */
		cmdstr := fmt.Sprintf("mkdir -p /home/sslclient/%s", conf.Name)
		err, _ = public.ExecBashCmdWithRet(cmdstr)
		if err != nil {
			agentLog.AgentLogger.Info(cmdstr, err)
			return err
		}

		/* 创建sslclient日志目录 */
		cmdstr = "mkdir -p /var/log/sslclient/"
		err, _ = public.ExecBashCmdWithRet(cmdstr)
		if err != nil {
			agentLog.AgentLogger.Info(cmdstr, err)
			return err
		}

		/* 初始化env文件 */
		if err := creatEnvFile(conf.Name, clientNameDef); err != nil {
			agentLog.AgentLogger.Info("CreateSslServer creatEnvFile failed")
			return err
		}

		/* 生成ca.crt文件 */
		fileName, err := CaConfCreate(conf)
		if err != nil {
			agentLog.AgentLogger.Info("CaConfCreate failed ", fileName)
			return err
		}

		/* init参数 */
		if strings.Contains(strings.ToLower(conf.Protocol), strings.ToLower("udp")) {
			conf.Proto = "udp"
		} else {
			conf.Proto = "tcp"
		}

		/* 生成conf 文件 */
		fileName, err = ClientConfCreate(conf)
		if err != nil {
			agentLog.AgentLogger.Info("ClientConfCreate failed ", fileName)
			return err
		}

		/* 生成账号密码文件 */
		fileName, err = PwsConfCreate(conf)
		if err != nil {
			agentLog.AgentLogger.Info("PwsConfCreate failed ", fileName)
			return err
		}
	}

	if err = resetSSL(conf.Name, conf.Bandwidth); err != nil {
		agentLog.AgentLogger.Info("ssl Create: resetSSL failed", conf.Name, err)
		return err
	}

	if conf.Nat {
		if err := public.SetInterfaceSnat(false, conf.Name); err != nil {
			return derr.Error{In: err.Error(), Out: "SetXfrmSnatError"}
		}

		if len(conf.NoNatCidr) != 0 {
			for _, cidr := range conf.NoNatCidr {
				if err := public.SetInterfaceNoSnat(false, conf.Name, cidr); err != nil {
					return derr.Error{In: err.Error(), Out: "SetXfrmNoSnatError"}
				}
			}
		}
	}

	return nil
}

func (cfgCur *SslConf) Modify(cfgNew *SslConf) (error, bool) {
	var err error
	var serverChg = false
	var chg = false

	if cfgCur.TunnelDst != cfgNew.TunnelDst {
		cfgCur.TunnelDst = cfgNew.TunnelDst
		chg = true
		serverChg = true
	}
	if cfgCur.Port != cfgNew.Port {
		cfgCur.Port = cfgNew.Port
		chg = true
		serverChg = true
	}
	if cfgCur.Protocol != cfgNew.Protocol {
		if strings.Contains(strings.ToLower(cfgNew.Protocol), strings.ToLower("udp")) {
			cfgNew.Proto = "udp"
		} else {
			cfgNew.Proto = "tcp"
		}
		cfgCur.Proto = cfgNew.Proto
		cfgCur.Protocol = cfgNew.Protocol
		chg = true
		serverChg = true
	}

	if cfgCur.Bandwidth != cfgNew.Bandwidth {
		cfgCur.Bandwidth = cfgNew.Bandwidth
		if err := public.SetInterfaceIngressLimit(cfgCur.Name, cfgCur.Bandwidth); err != nil {
			agentLog.AgentLogger.Info("SetInterfaceIngressLimit failed", cfgCur.Name, err)
			return err, true
		}
		chg = true
	}

	if cfgCur.Username != cfgNew.Username || cfgCur.Passwd != cfgNew.Passwd {
		cfgCur.Username = cfgNew.Username
		cfgCur.Passwd = cfgNew.Passwd
		/* 重新生成账号密码文件 */
		fileName, err := PwsConfCreate(cfgCur)
		if err != nil {
			agentLog.AgentLogger.Info("PwsConfCreate failed ", fileName)
			return err, false
		}
		chg = true
		serverChg = true
	}

	if cfgCur.Nat != cfgNew.Nat {
		/* set snat */
		if cfgCur.Nat {
			err = public.SetInterfaceSnat(true, cfgCur.Name)
			if err != nil {
				return err, false
			}

			if len(cfgCur.NoNatCidr) != 0 {
				for _, cidr := range cfgCur.NoNatCidr {
					if err := public.SetInterfaceNoSnat(true, cfgCur.Name, cidr); err != nil {
						return err, false
					}
				}
			}
		} else if cfgNew.Nat {
			err = public.SetInterfaceSnat(false, cfgCur.Name)
			if err != nil {
				return err, false
			}

			if len(cfgNew.NoNatCidr) != 0 {
				for _, cidr := range cfgNew.NoNatCidr {
					if err := public.SetInterfaceNoSnat(false, cfgCur.Name, cidr); err != nil {
						return err, false
					}
				}
			}
		}
		cfgCur.Nat = cfgNew.Nat
		cfgCur.NoNatCidr = cfgNew.NoNatCidr
		chg = true
	}

	if cfgCur.Nat {
		/* 如果是开启SNAT，检查NoNatCidr是否有改变 */
		add, delete := public.Arrcmp(cfgCur.NoNatCidr, cfgNew.NoNatCidr)
		if len(add) != 0 || len(delete) != 0 {
			if len(delete) != 0 {
				/* del */
				for _, cidr := range delete {
					if err := public.SetInterfaceNoSnat(true, cfgCur.Name, cidr); err != nil {
						return err, false
					}
				}
			}
			if len(add) != 0 {
				/* add */
				for _, cidr := range add {
					if err := public.SetInterfaceNoSnat(false, cfgCur.Name, cidr); err != nil {
						return err, false
					}
				}
			}
			cfgCur.NoNatCidr = cfgNew.NoNatCidr
			chg = true
		}
	}

	if serverChg {
		fileName, err := ClientConfCreate(cfgCur)
		agentLog.AgentLogger.Info("ClientConfCreate: SSLCfgCur: ", cfgCur)
		if err != nil {
			agentLog.AgentLogger.Info("ClientConfCreate failed ", fileName)
			return nil, true
		}

		if err := resetSSL(cfgCur.Name, cfgCur.Bandwidth); err != nil {
			agentLog.AgentLogger.Info("SSL Modify: resetSSL  failed", cfgCur.Name, err)
			return err, true
		}
	}

	return nil, chg
}

func (conf *SslConf) Destroy() error {
	var err error

	if conf.Nat {
		/* delete snat */
		if err := public.SetInterfaceSnat(true, conf.Name); err != nil {
			return derr.Error{In: err.Error(), Out: "SetXfrmSnatError"}
		}

		if len(conf.NoNatCidr) != 0 {
			for _, cidr := range conf.NoNatCidr {
				if err := public.SetInterfaceNoSnat(true, conf.Name, cidr); err != nil {
					return derr.Error{In: err.Error(), Out: "SetXfrmNoSnatError"}
				}
			}
		}
	}

	//关闭openVpn进程
	if err = sslclientStop(conf.Name, clientNameDef); err != nil {
		return err
	}

	//删除sslclient相关的目录
	dirPath := fmt.Sprintf(sslNsPath, conf.Name)
	if public.FileExists(dirPath) {
		os.RemoveAll(dirPath)
	}

	//删除sslclient日志的目录
	dirPath = "/var/log/sslclient/"
	if public.FileExists(dirPath) {
		os.RemoveAll(dirPath)
	}

	//删除env文件
	EnvPath := fmt.Sprintf(clientEnvPath, conf.Name, clientNameDef)
	if public.FileExists(EnvPath) {
		os.Remove(EnvPath)
	}

	return nil
}
