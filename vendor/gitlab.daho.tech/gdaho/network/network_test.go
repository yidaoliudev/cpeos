package network

import (
	"reflect"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestOvsPortName(t *testing.T) {
	type args struct {
		uid string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		// TODO: Add test cases.
		{"good", args{"test1"}, "test1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OvsPortName(tt.args.uid); got != tt.want {
				t.Errorf("OvsPortName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNSPortName(t *testing.T) {
	type args struct {
		uid string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		// TODO: Add test cases.
		{"good", args{"test1"}, "test1_n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NSPortName(tt.args.uid); got != tt.want {
				t.Errorf("NSPortName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeleteLink(t *testing.T) {
	//NewVethLink("test1")
	type args struct {
		uid string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
		{"good", args{"test1"}, false},
		{"bad", args{"test1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := DeleteOvsLink(tt.args.uid); (err != nil) != tt.wantErr {
				t.Errorf("DeleteOvsLink() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConnectedLinks(t *testing.T) {
	AddrFlush("eth0")
	AddrFlush("eth2")
	wants := []netlink.Link{}
	eth0, _ := netlink.LinkByName("eth0")
	eth1, _ := netlink.LinkByName("eth1")
	wants = append(wants, eth0)
	wants = append(wants, eth1)
	tests := []struct {
		name    string
		want    []netlink.Link
		wantErr bool
	}{
		// TODO: Add test cases.
		{"good1", wants, false},
		{"bad1", wants, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConnectedLinks()
			if (err != nil) != tt.wantErr {
				t.Errorf("ConnectedLinks() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConnectedLinks() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckIPOnLink(t *testing.T) {
	//eth2, _ := netlink.LinkByName("eth2")
	type args struct {
		local string
		peer  string
		link  netlink.Link
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckIPOnLink(tt.args.local, tt.args.peer, tt.args.link); got != tt.want {
				t.Errorf("CheckIPOnLink() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLinkSetNS(t *testing.T) {
	type args struct {
		name string
		uid  string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := LinkSetNS(tt.args.name, tt.args.uid); (err != nil) != tt.wantErr {
				t.Errorf("LinkSetNS() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigRoute(t *testing.T) {
	//	type args struct {
	//		routes []Route
	//		action string
	//	}
	//	tests := []struct {
	//		name    string
	//		args    args
	//		wantErr bool
	//	}{
	//	// TODO: Add test cases.
	//	//{"good1", args{[]Route{{"0.0.0.0/0", ""}}, ActionDel}, false},
	//	//{"good2", args{[]Route{{"8.8.8.8/32", "172.16.0.254"}}, ActionAdd}, false},
	//	}
	//	for _, tt := range tests {
	//		t.Run(tt.name, func(t *testing.T) {
	//			if err := ConfigRoute(tt.args.routes, tt.args.action); (err != nil) != tt.wantErr {
	//				t.Errorf("ConfigRoute() error = %v, wantErr %v", err, tt.wantErr)
	//			}
	//		})
	//	}
}

func TestAssignIP(t *testing.T) {
	type args struct {
		nic string
		ip  string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := AssignIP(tt.args.nic, tt.args.ip); (err != nil) != tt.wantErr {
				t.Errorf("AssignIP() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeleteIP(t *testing.T) {
	type args struct {
		nic string
		ip  string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := DeleteIP(tt.args.nic, tt.args.ip); (err != nil) != tt.wantErr {
				t.Errorf("DeleteIP() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAddrFlush(t *testing.T) {
	type args struct {
		name string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := AddrFlush(tt.args.name); (err != nil) != tt.wantErr {
				t.Errorf("AddrFlush() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLinkDownAndUp(t *testing.T) {
	type args struct {
		name string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := LinkDownAndUp(tt.args.name); (err != nil) != tt.wantErr {
				t.Errorf("LinkDownAndUp() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInitLink(t *testing.T) {
	type args struct {
		uid string
		mac string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := InitLink(tt.args.uid, tt.args.mac); (err != nil) != tt.wantErr {
				t.Errorf("InitLink() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeleteVlanLink(t *testing.T) {
	type args struct {
		vlanID int
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := DeleteVlanLink(tt.args.vlanID, DefaultName); (err != nil) != tt.wantErr {
				t.Errorf("DeleteVlanLink() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewVlanLink(t *testing.T) {
	type args struct {
		vlanID int
		ip     string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := NewVlanLink(tt.args.vlanID, tt.args.ip, DefaultName); (err != nil) != tt.wantErr {
				t.Errorf("NewVlanLink() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

//func TestNewVethLink(t *testing.T) {
//	type args struct {
//		uid string
//	}
//	tests := []struct {
//		name    string
//		args    args
//		wantErr bool
//	}{
//	// TODO: Add test cases.
//	}
//	for _, tt := range tests {
//		t.Run(tt.name, func(t *testing.T) {
//			if err := NewVethLink(tt.args.uid); (err != nil) != tt.wantErr {
//				t.Errorf("NewVethLink() error = %v, wantErr %v", err, tt.wantErr)
//			}
//		})
//	}
//}

func TestPersistentNetwork(t *testing.T) {
	type args struct {
		eths []NIC
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
		{"good", args{[]NIC{{Dev: "eth0"}, {Dev: "eth1"}}}, false},
		{"good", args{[]NIC{{Dev: "eth0"}, {Dev: "eth1", Addr: "172.16.0.0", Prefix: "172.16.0.0/24"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := PersistentNetwork(tt.args.eths); (err != nil) != tt.wantErr {
				t.Errorf("PersistentNetwork() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigRouteZebra(t *testing.T) {
	r1 := &Route{Prefix: "2.2.2.0/24", Nexthop: "192.168.121.1", TableId: 110}
	r2 := &Route{Prefix: "3.3.3.0/24", Nexthop: "192.168.121.1", TableId: 110}
	type args struct {
		route  *Route
		action string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
		{"good1", args{r1, ActionAdd}, false},
		//{"good2", args{r1, ActionAdd}, true},
		{"good3", args{r2, ActionAdd}, false},
		//{"good4", args{r1, ActionDel}, false},
		//{"good5", args{r2, ActionDel}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ConfigRouteZebra(tt.args.route, tt.args.action); (err != nil) != tt.wantErr {
				t.Errorf("ConfigRouteZebra() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRuleList(t *testing.T) {

	rule := &Rule{From: "1.1.1.1/24", TableId: 20, Pref: 30000}
	err := ConfigRule(rule, ActionAdd)
	if err != nil {
		t.Errorf("TestRuleList ConfigRule error. error = %v", err)
		return
	}

	type args struct {
		pref int
		from string
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		// TODO: Add test cases.
		{"good1", args{30000, "1.1.1.1/24"}, true, false},
		{"bad1", args{30001, "2.1.1.1/24"}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RuleList(tt.args.pref, tt.args.from)
			if (err != nil) != tt.wantErr {
				t.Errorf("RuleList() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("RuleList() = %v, want %v", got, tt.want)
			}
		})
	}
	err = ConfigRule(rule, ActionDel)
	if err != nil {
		t.Errorf("TestRuleList ConfigRule error. error = %v", err)
		return
	}
}
