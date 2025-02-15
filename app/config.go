package app

type AllConf struct {
	Ts            int64        `json:"ts"`
	SiteConfig    SiteConf     `json:"siteConfig"`
	PortConfig    []PortConf   `json:"portConfig"`
	SubnetConfig  []SubnetConf `json:"subnetConfig"`
	ConnConfig    []ConnConf   `json:"connConfig"`
	StaticConfig  []StaticConf `json:"staticConfig"`
	CheckConfig   []CheckConf  `json:"checkConfig"`
	BgpConfig     BgpConf      `json:"bgpConfig"`
	DnsConfig     DnsConf      `json:"dnsConfig"`
	DhcpConfig    DhcpConf     `json:"dhcpConfig"`
	HaConfig      HaConf       `json:"haConfig"`
	GeneralConfig GeneralConf  `json:"generalConfig"`
}

type DpConfig struct {
	Success bool    `json:"success"`
	Ret     int     `json:"ret"`
	Code    string  `json:"code"`
	Msg     string  `json:"msg"`
	Data    AllConf `json:"data"`
}

/*
## 更新CPE全量配置
{
    "ts":12928839299223,
    "siteConfig":{
        "id":"site-00001"
    },
    "portConfig":[
        {
            "id":"wan1"
        },
        {
            "id":"lan1",
            "ipAddr":"172.16.20.2/24",
            "nexthop":"172.16.20.1"
        }
    ],
    "subnetConfig":[
        {
            "id":"lan1",
            "cidrs":[
                "10.23.0.0/16",
                "10.24.0.0/16",
                "10.25.0.0/16"
            ]
        }
    ],
    "ipsecConfig":[
        {
            "id":"ipsec-0001",
            "xfrmId":1,
            "leftFqdn":"ipsec-bf828a0a",
            "rightFqdn":"mce-ipsec-bf828a0a",
            "bandwidth":0,
            "healthCheck":true,
            "localAddress":"192.168.100.1",
            "remoteAddress":"192.168.100.2",
            "ipsecSecret":"337e72f630d6534627d1ee27128311",
            "remoteGateway":"129.227.152.6",
            "mainMode":false,
            "lifeTime":"3600s",
            "rekeyTime":"3960s"
        },
        {
            "id":"ipsec-0002",
            "xfrmId":2,
            "leftFqdn":"ipsec-bf828a0b",
            "rightFqdn":"mce-ipsec-bf828a0b",
            "bandwidth":0,
            "healthCheck":true,
            "localAddress":"192.168.100.5",
            "remoteAddress":"192.168.100.6",
            "ipsecSecret":"337e72f630d6534627d1ee27128312",
            "remoteGateway":"129.227.152.112",
            "mainMode":false,
            "lifeTime":"3600s",
            "rekeyTime":"3960s"
        }
    ],
    "staticConfig":[
        {
            "id":"172.16.21.0/24",
            "device":"lan1"
        },
        {
            "id":"10.200.0.0/20",
            "device":"ipsec-0001"
        }
    ],
    "bgpConfig":{
        "localAs":65539,
        "neighConfig":[
            {
                "id":"bgp-0001",
                "peerAddress":"192.168.100.2",
                "peerAs":65541,
                "ebgpMutihop":255,
                "keepAlive":60,
                "holdTime":180,
                "password":""
            },
            {
                "id":"bgp-0002",
                "peerAddress":"192.168.100.6",
                "peerAs":65541,
                "ebgpMutihop":255,
                "keepAlive":60,
                "holdTime":180,
                "password":""
            },
            {
                "id":"bgp-0003",
                "peerAddress":"172.16.20.100",
                "peerAs":65542,
                "ebgpMutihop":255,
                "keepAlive":60,
                "holdTime":180,
                "password":""
            }
        ]
    },
    "checkConfig":[
        {
            "id":"8.8.8.8",
            "device":"wan1"
        }
    ],
    "dnsConfig":{
        "enable":true,
        "listenAddress":"172.16.20.2",
        "cacheSize":1000,
        "primaryDNS":"116.228.111.118",
        "secondaryDNS":"180.168.255.18",
        "proxyDNS":"8.8.8.8",
        "forced":true
    },
    "haConfig":{
        "enable":true,
        "role":1,
        "portVip":[
            {
                "id":"wan1",
                "ipAddr":"192.168.31.100/24"
            },
            {
                "id":"lan1",
                "ipAddr":"172.16.20.100/24"
            }
        ]
    }
}
*/
