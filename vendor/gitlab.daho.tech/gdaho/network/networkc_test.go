package network

import (
//"C"
)

import "testing"

func TestListenPort(t *testing.T) {
	type args struct {
		ip   string
		port int
		time int
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
		{"case1bad", args{"1.1.1.1", 80, 3}, true},
		{"case2good", args{"172.16.0.131", 80, 3}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ListenPort(tt.args.ip, tt.args.port, tt.args.time); (err != nil) != tt.wantErr {
				t.Errorf("ListenPort() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
