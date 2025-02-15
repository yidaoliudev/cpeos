package network

import (
	"path"
	"syscall"

	"github.com/vishvananda/netns"
)

const (
	netns_run_dir = "/var/run/netns"
	origin        = "origin"
)

var (
	initNs netns.NsHandle
)

func Init() {
	var err error
	if initNs, err = netns.Get(); err != nil {
		panic(err)
	}
}

func DestroyNamespace(uid string) error {
	path := path.Join(netns_run_dir, uid)
	err := netns.Delete(path)
	if err != nil {
		return err
	}
	return nil
}

func CreateNamespace(uid string) error {
	path := path.Join(netns_run_dir, uid)
	_, err := netns.NewAndSave(path)
	if err != nil {
		return err
	}
	return nil
}

func SaveOriginNamespace() error {
	path := path.Join(netns_run_dir, origin)
	_, err := netns.Save(path)
	if err != nil {
		return err
	}
	return nil
}

func SwitchNS(nsName string) error {
	ns, err := netns.GetFromName(nsName)
	defer syscall.Close(int(ns))
	if err != nil {
		return err
	}
	err = netns.Set(ns)
	if err != nil {
		return err
	}
	return nil

}

func SwitchOriginNS() error {
	err := netns.Set(initNs)
	if err != nil {
		return err
	}
	return nil
}

func GetFromName(uid string) (int, error) {
	ns, err := netns.GetFromName(uid)
	return int(ns), err
}

func SwitchNSByPid(pid int) error {
	ns, err := netns.GetFromPid(pid)
	defer syscall.Close(int(ns))
	if err != nil {
		return err
	}
	err = netns.Set(ns)
	if err != nil {
		return err
	}
	return nil

}

func GetOriginNs() netns.NsHandle {
	return initNs
}