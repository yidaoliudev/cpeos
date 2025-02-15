package network

import "testing"

func init() {
	Init()
}

func TestInit(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Init(); (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDestroyNamespace(t *testing.T) {
	CreateNamespace("test1")
	type args struct {
		uid string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
		{"good1", args{"test1"}, false},
		{"bad1", args{"test1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := DestroyNamespace(tt.args.uid); (err != nil) != tt.wantErr {
				t.Errorf("DestroyNamespace() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCreateNamespace(t *testing.T) {
	DestroyNamespace("test1")
	type args struct {
		uid string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
		{"good1", args{"test1"}, false},
		{"bad1", args{"test1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CreateNamespace(tt.args.uid); (err != nil) != tt.wantErr {
				t.Errorf("CreateNamespace() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSaveOriginNamespace(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := SaveOriginNamespace(); (err != nil) != tt.wantErr {
				t.Errorf("SaveOriginNamespace() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSwitchNS(t *testing.T) {
	type args struct {
		uid string
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
			if err := SwitchNS(tt.args.uid); (err != nil) != tt.wantErr {
				t.Errorf("SwitchNS() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSwitchOriginNS(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := SwitchOriginNS(); (err != nil) != tt.wantErr {
				t.Errorf("SwitchOriginNS() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetFromName(t *testing.T) {
	type args struct {
		uid string
	}
	tests := []struct {
		name    string
		args    args
		want    int
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetFromName(tt.args.uid)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetFromName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetFromName() = %v, want %v", got, tt.want)
			}
		})
	}
}
