package kvmanager

import (
	"context"
	"fmt"
	"strings"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// ClientV3 封装 etcd v3 客户端操作（per-request 模式，每次操作创建临时连接）
type ClientV3 struct {
	cfg       *config.Config
	healthMgr *health.Manager
	separator string
}

// NewClientV3 创建 v3 客户端实例
func NewClientV3(cfg *config.Config, healthMgr *health.Manager) (*ClientV3, error) {
	return &ClientV3{
		cfg:       cfg,
		healthMgr: healthMgr,
		separator: cfg.KVManager.Separator,
	}, nil
}

// Close per-request 模式下无需全局关闭
func (c *ClientV3) Close() error {
	return nil
}

// newClient 创建临时 etcd 客户端，调用方负责 defer cli.Close()
func (c *ClientV3) newClient() (*clientv3.Client, error) {
	etcdCfg := clientv3.Config{
		Endpoints:   c.healthMgr.HealthyEndpoints(),
		DialTimeout: time.Duration(c.cfg.KVManager.ConnectTimeout) * time.Second,
	}
	if c.cfg.Etcd.Username != "" {
		etcdCfg.Username = c.cfg.Etcd.Username
		etcdCfg.Password = c.cfg.Etcd.Password
	}
	return clientv3.New(etcdCfg)
}

// GetSeparator 返回路径分隔符
func (c *ClientV3) GetSeparator() string {
	return c.separator
}

// Connect 获取集群连接信息
func (c *ClientV3) Connect() (*ConnectInfo, error) {
	cli, err := c.newClient()
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	// 通过健康管理器获取 Status
	statusResp, err := c.healthMgr.StatusFromHealthy(cli)
	if err != nil {
		return nil, fmt.Errorf("get status from healthy endpoints: %w", err)
	}

	memberResp, err := cli.MemberList(ctx)
	if err != nil {
		return nil, err
	}

	leaderName := ""
	for _, m := range memberResp.Members {
		if m.ID == statusResp.Leader {
			leaderName = m.Name
			break
		}
	}

	size := statusResp.DbSize
	return &ConnectInfo{
		Version: statusResp.Version,
		Name:    leaderName,
		Size:    size,
		SizeStr: formatSize(size),
	}, nil
}

// Get 获取单个 key 的值
func (c *ClientV3) Get(key string) (*Node, error) {
	cli, err := c.newClient()
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	resp, err := cli.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	kv := resp.Kvs[0]
	ttl := c.getTTLWithClient(cli, kv.Lease)

	return &Node{
		Key:            string(kv.Key),
		Value:          string(kv.Value),
		Dir:            false,
		TTL:            ttl,
		CreateRevision: kv.CreateRevision,
		ModRevision:    kv.ModRevision,
		Version:        kv.Version,
	}, nil
}

// GetPath 获取指定路径下的子节点（树结构）
func (c *ClientV3) GetPath(key string) (*Node, error) {
	cli, err := c.newClient()
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	prefixKey := key
	if key == c.separator {
		prefixKey = ""
	}

	resp, err := cli.Get(ctx, prefixKey, clientv3.WithPrefix(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	if err != nil {
		return nil, err
	}

	rootNode := &Node{
		Key:   key,
		Dir:   true,
		Nodes: []Node{},
	}

	if len(resp.Kvs) == 0 {
		return rootNode, nil
	}

	type nodeEntry struct {
		key            string
		value          string
		ttl            int64
		createRevision int64
		modRevision    int64
		version        int64
		isRealKey      bool
		children       map[string]*nodeEntry
		childOrder     []string
	}

	root := &nodeEntry{
		key:      key,
		children: make(map[string]*nodeEntry),
	}

	var prefixSegCount int
	if key != c.separator && key != "" {
		for _, p := range strings.Split(key, c.separator) {
			if p != "" {
				prefixSegCount++
			}
		}
	}

	for _, kv := range resp.Kvs {
		kvKey := string(kv.Key)

		rawParts := strings.Split(kvKey, c.separator)
		segments := make([]string, 0, len(rawParts))
		for _, p := range rawParts {
			if p != "" {
				segments = append(segments, p)
			}
		}

		if len(segments) == 0 {
			root.isRealKey = true
			root.value = string(kv.Value)
			root.ttl = c.getTTLWithClient(cli, kv.Lease)
			root.createRevision = kv.CreateRevision
			root.modRevision = kv.ModRevision
			root.version = kv.Version
			continue
		}

		if kvKey == key {
			root.isRealKey = true
			root.value = string(kv.Value)
			root.ttl = c.getTTLWithClient(cli, kv.Lease)
			root.createRevision = kv.CreateRevision
			root.modRevision = kv.ModRevision
			root.version = kv.Version
			continue
		}

		relSegs := segments[prefixSegCount:]
		if len(relSegs) == 0 {
			continue
		}

		current := root
		for i, seg := range relSegs {
			child, exists := current.children[seg]
			if !exists {
				childKey := c.separator + strings.Join(segments[:prefixSegCount+i+1], c.separator)
				child = &nodeEntry{
					key:      childKey,
					children: make(map[string]*nodeEntry),
				}
				current.children[seg] = child
				current.childOrder = append(current.childOrder, seg)
			}
			current = child
		}

		current.isRealKey = true
		current.value = string(kv.Value)
		current.ttl = c.getTTLWithClient(cli, kv.Lease)
		current.createRevision = kv.CreateRevision
		current.modRevision = kv.ModRevision
		current.version = kv.Version
	}

	var convertEntry func(e *nodeEntry) Node
	convertEntry = func(e *nodeEntry) Node {
		node := Node{
			Key:            e.key,
			Value:          e.value,
			TTL:            e.ttl,
			CreateRevision: e.createRevision,
			ModRevision:    e.modRevision,
			Version:        e.version,
		}

		if len(e.children) > 0 {
			node.Dir = true
			node.Nodes = make([]Node, 0, len(e.children))
			for _, name := range e.childOrder {
				if child, ok := e.children[name]; ok {
					node.Nodes = append(node.Nodes, convertEntry(child))
				}
			}
		} else if !e.isRealKey {
			node.Dir = true
			node.Nodes = []Node{}
		} else {
			node.Dir = false
		}

		return node
	}

	rootNode.Nodes = make([]Node, 0, len(root.children))
	for _, name := range root.childOrder {
		if child, ok := root.children[name]; ok {
			rootNode.Nodes = append(rootNode.Nodes, convertEntry(child))
		}
	}

	if root.isRealKey {
		rootNode.Value = root.value
		rootNode.TTL = root.ttl
		rootNode.CreateRevision = root.createRevision
		rootNode.ModRevision = root.modRevision
		rootNode.Version = root.version
	}

	return rootNode, nil
}

// Put 创建或更新 key
func (c *ClientV3) Put(key, value string, ttl int64) (*Node, error) {
	cli, err := c.newClient()
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	var opts []clientv3.OpOption

	if ttl > 0 {
		leaseResp, err := cli.Grant(ctx, ttl)
		if err != nil {
			return nil, fmt.Errorf("create lease failed: %w", err)
		}
		opts = append(opts, clientv3.WithLease(leaseResp.ID))
	}

	_, err = cli.Put(ctx, key, value, opts...)
	if err != nil {
		return nil, err
	}

	// 读回确认
	resp, err := cli.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("key not found after put: %s", key)
	}

	kv := resp.Kvs[0]
	ttlVal := c.getTTLWithClient(cli, kv.Lease)

	return &Node{
		Key:            string(kv.Key),
		Value:          string(kv.Value),
		Dir:            false,
		TTL:            ttlVal,
		CreateRevision: kv.CreateRevision,
		ModRevision:    kv.ModRevision,
		Version:        kv.Version,
	}, nil
}

// Delete 删除 key 或目录
func (c *ClientV3) Delete(key string, dir bool) error {
	cli, err := c.newClient()
	if err != nil {
		return err
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	_, err = cli.Delete(ctx, key)
	if err != nil {
		return err
	}

	if dir {
		prefix := key + c.separator
		if key == c.separator {
			prefix = c.separator
		}
		_, err = cli.Delete(ctx, prefix, clientv3.WithPrefix())
		if err != nil {
			return err
		}
	}

	return nil
}

// getTTLWithClient 使用已有客户端获取 lease 的剩余 TTL
func (c *ClientV3) getTTLWithClient(cli *clientv3.Client, leaseID int64) int64 {
	if leaseID == 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	resp, err := cli.Lease.TimeToLive(ctx, clientv3.LeaseID(leaseID))
	if err != nil {
		return 0
	}
	if resp.TTL == -1 {
		return 0
	}
	return resp.TTL
}

// requestTimeout 返回配置的请求超时时间
func (c *ClientV3) requestTimeout() time.Duration {
	return time.Duration(c.cfg.KVManager.RequestTimeout) * time.Second
}
