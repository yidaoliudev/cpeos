package etcd

import (
	"strings"

	"github.com/coreos/etcd/client"

	dahoetcd "gitlab.daho.tech/gdaho/etcd"
)

type Client struct {
	client client.KeysAPI
}

var (
	EtcdClient *dahoetcd.Client
)

const (
	etcdServer = "127.0.0.1:2379"
)

func Etcdinit() error {
	ips := make([]string, 1)
	ips[0] = "http://" + etcdServer
	var err error
	EtcdClient, err = dahoetcd.NewEtcdClient(ips, "", "", "", false, "", "")
	return err
}

func EtcdGetValue(path string) (string, error) {
	return EtcdClient.GetValue(path)
}

func EtcdIsExistValue(path string) (string, bool, error) {
	val, err := EtcdClient.GetValue(path)
	if err != nil {
		errCode := err.Error() // etcd may be not vll
		if strings.EqualFold("100", strings.Split(errCode, ":")[0]) {
			return "", false, nil
		} else {
			return "", false, err
		}
	}

	return val, true, nil
}

func EtcdGetValueWithCheck(path string) (string, bool, error) {
	val, err := EtcdClient.GetValue(path)
	if err != nil {
		errCode := err.Error() // etcd may be not vll
		if strings.EqualFold("100", strings.Split(errCode, ":")[0]) {
			return "", false, nil
		} else {
			return "", false, err
		}
	}

	return val, true, nil
}

func EtcdSetValue(path string, data string) error {
	return EtcdClient.SetValue(path, data)
}

func EtcdDelValue(path string) error {
	return EtcdClient.DelValue(path)
}

func EtcdGetValues(paths []string) (map[string]string, error) {
	return EtcdClient.GetValues(paths)
}

func EtcdGetValuesWithCheck(paths []string) (map[string]string, bool, error) {
	vals, err := EtcdClient.GetValues(paths)
	if err != nil {
		errCode := err.Error() // etcd may be not vll
		if strings.EqualFold("100", strings.Split(errCode, ":")[0]) {
			return nil, false, nil
		} else {
			return nil, false, err
		}
	}
	return vals, true, nil
}
