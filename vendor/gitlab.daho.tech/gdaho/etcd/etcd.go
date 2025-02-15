package etcd

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
)

// Client is a wrapper around the etcd client
type Client struct {
	client client.KeysAPI
}

// NewEtcdClient returns an *etcd.Client with a connection to named machines.
func NewEtcdClient(machines []string, cert, key, caCert string, basicAuth bool, username string, password string) (*Client, error) {
	var c client.Client
	var kapi client.KeysAPI
	var err error
	var transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
	}

	cfg := client.Config{
		Endpoints:               machines,
		HeaderTimeoutPerRequest: time.Duration(3) * time.Second,
	}

	if basicAuth {
		cfg.Username = username
		cfg.Password = password
	}

	if caCert != "" {
		certBytes, err := ioutil.ReadFile(caCert)
		if err != nil {
			return &Client{kapi}, err
		}

		caCertPool := x509.NewCertPool()
		ok := caCertPool.AppendCertsFromPEM(certBytes)

		if ok {
			tlsConfig.RootCAs = caCertPool
		}
	}

	if cert != "" && key != "" {
		tlsCert, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return &Client{kapi}, err
		}
		tlsConfig.Certificates = []tls.Certificate{tlsCert}
	}

	transport.TLSClientConfig = tlsConfig
	cfg.Transport = transport

	c, err = client.New(cfg)
	if err != nil {
		return &Client{kapi}, err
	}

	kapi = client.NewKeysAPI(c)
	return &Client{kapi}, nil
}

func (c *Client) GetValue(key string) (string, error) {
	resp, err := c.client.Get(context.Background(), key, nil)
	if err != nil {
		return "", err
	}
	return resp.Node.Value, nil
}

func (c *Client) DelValue(key string) error {
	_, err := c.client.Delete(context.Background(), key, &client.DeleteOptions{
		Recursive: true,
		Dir:       true})
	return err
}

// GetValues queries etcd for keys prefixed by prefix.
func (c *Client) GetValues(keys []string) (map[string]string, error) {
	vars := make(map[string]string)
	for _, key := range keys {
		resp, err := c.client.Get(context.Background(), key, &client.GetOptions{
			Recursive: true,
			Sort:      true,
			Quorum:    true,
		})
		if err != nil {
			return vars, err
		}
		err = nodeWalk(resp.Node, vars)
		if err != nil {
			return vars, err
		}
	}
	return vars, nil
}

func (c *Client) SetValue(key, value string) error {
	_, err := c.client.Set(context.Background(), key, value, &client.SetOptions{})
	if err != nil {
		return err
	}
	return nil
}

// SetValues set value for keys prefixed by prefix.
func (c *Client) SetValues(m map[string]string) error {
	for key, value := range m {
		_, err := c.client.Set(context.Background(), key, value, &client.SetOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

// nodeWalk recursively descends nodes, updating vars.
func nodeWalk(node *client.Node, vars map[string]string) error {
	if node != nil {
		key := node.Key
		if !node.Dir {
			vars[key] = node.Value
		} else {
			for _, node := range node.Nodes {
				nodeWalk(node, vars)
			}
		}
	}
	return nil
}

func (c *Client) WatchPrefix(prefix string, keys []string, waitIndex uint64, stopChan chan bool) (uint64, error) {
	// return something > 0 to trigger a key retrieval from the store
	if waitIndex == 0 {
		return 1, nil
	}

	for {
		// Setting AfterIndex to 0 (default) means that the Watcher
		// should start watching for events starting at the current
		// index, whatever that may be.
		watcher := c.client.Watcher(prefix, &client.WatcherOptions{AfterIndex: uint64(0), Recursive: true})
		ctx, cancel := context.WithCancel(context.Background())
		cancelRoutine := make(chan bool)
		defer close(cancelRoutine)

		go func() {
			select {
			case <-stopChan:
				cancel()
			case <-cancelRoutine:
				return
			}
		}()

		resp, err := watcher.Next(ctx)
		if err != nil {
			switch e := err.(type) {
			case *client.Error:
				if e.Code == 401 {
					return 0, nil
				}
			}
			return waitIndex, err
		}

		// Only return if we have a key prefix we care about.
		// This is not an exact match on the key so there is a chance
		// we will still pickup on false positives. The net win here
		// is reducing the scope of keys that can trigger updates.
		for _, k := range keys {
			if strings.HasPrefix(resp.Node.Key, k) {
				return resp.Node.ModifiedIndex, err
			}
		}
	}
}

/* 返回错误会推出watch */
type WatchCallback func(*client.Response, interface{}) error

func (c *Client) WatchProc(ctx context.Context,
	key string,
	afterIndex uint64,
	callback WatchCallback,
	callbackPara interface{}) error {

	// Setting AfterIndex to 0 (default) means that the Watcher
	// should start watching for events starting at the current
	// index, whatever that may be.
	watcher := c.client.Watcher(key, &client.WatcherOptions{AfterIndex: afterIndex, Recursive: true})

	for {
		resp, err := watcher.Next(ctx)
		if err != nil {
			switch e := err.(type) {
			case *client.Error:
				if e.Code == 401 {
					return nil
				}
			}
			return err
		}

		err = callback(resp, callbackPara)
		if err != nil {
			return err
		}
	}
}
