package kvmanager

import (
	"context"
	"fmt"
	"strings"
	"time"

	"etcdmonitor/internal/config"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// ClientV3 封装 etcd v3 客户端操作
type ClientV3 struct {
	client    *clientv3.Client
	cfg       *config.Config
	separator string
}

// NewClientV3 创建 v3 客户端实例
func NewClientV3(cfg *config.Config) (*ClientV3, error) {
	timeout := time.Duration(cfg.KVManager.ConnectTimeout) * time.Second

	etcdCfg := clientv3.Config{
		Endpoints:   []string{cfg.Etcd.Endpoint},
		DialTimeout: timeout,
	}
	if cfg.Etcd.Username != "" {
		etcdCfg.Username = cfg.Etcd.Username
		etcdCfg.Password = cfg.Etcd.Password
	}

	client, err := clientv3.New(etcdCfg)
	if err != nil {
		return nil, err
	}

	return &ClientV3{
		client:    client,
		cfg:       cfg,
		separator: cfg.KVManager.Separator,
	}, nil
}

// Close 关闭客户端连接
func (c *ClientV3) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// GetSeparator 返回路径分隔符
func (c *ClientV3) GetSeparator() string {
	return c.separator
}

// Connect 获取集群连接信息
func (c *ClientV3) Connect() (*ConnectInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	// 获取集群状态
	statusResp, err := c.client.Status(ctx, c.cfg.Etcd.Endpoint)
	if err != nil {
		return nil, err
	}

	// 获取成员列表，找到 Leader 名称
	memberResp, err := c.client.MemberList(ctx)
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
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	resp, err := c.client.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	kv := resp.Kvs[0]
	ttl := c.getTTL(kv.Lease)

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
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	// 构建前缀查询 key
	prefixKey := key
	if key == c.separator {
		prefixKey = ""
	}

	resp, err := c.client.Get(ctx, prefixKey, clientv3.WithPrefix(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	if err != nil {
		return nil, err
	}

	// 根节点
	rootNode := &Node{
		Key:   key,
		Dir:   true,
		Nodes: []Node{},
	}

	if len(resp.Kvs) == 0 {
		return rootNode, nil
	}

	// 用树形 map 构建层级结构
	// nodeEntry 跟踪每个路径节点的数据和子节点
	type nodeEntry struct {
		key            string
		value          string
		ttl            int64
		createRevision int64
		modRevision    int64
		version        int64
		isRealKey      bool                   // 是否为 etcd 中实际存在的 key（非虚拟中间目录）
		children       map[string]*nodeEntry   // 子节点名称 → entry
		childOrder     []string               // 子节点名称的插入顺序（保持排序）
	}

	root := &nodeEntry{
		key:      key,
		children: make(map[string]*nodeEntry),
	}

	// 计算查询 key 的前缀段数，用于跳过
	// "/" → 0 段，"/ioa" → 1 段 ["ioa"]，"/ioa/agent" → 2 段 ["ioa","agent"]
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

		// 将 key 拆分为路径段，过滤空字符串
		// "/ioa/agent/config" → ["ioa", "agent", "config"]
		rawParts := strings.Split(kvKey, c.separator)
		segments := make([]string, 0, len(rawParts))
		for _, p := range rawParts {
			if p != "" {
				segments = append(segments, p)
			}
		}

		if len(segments) == 0 {
			// key 就是 "/" 本身
			root.isRealKey = true
			root.value = string(kv.Value)
			root.ttl = c.getTTL(kv.Lease)
			root.createRevision = kv.CreateRevision
			root.modRevision = kv.ModRevision
			root.version = kv.Version
			continue
		}

		// 如果 kvKey 正好等于查询的 key（如 "/ioa" 本身也是一个 key），
		// 数据归属到 root 自身
		if kvKey == key {
			root.isRealKey = true
			root.value = string(kv.Value)
			root.ttl = c.getTTL(kv.Lease)
			root.createRevision = kv.CreateRevision
			root.modRevision = kv.ModRevision
			root.version = kv.Version
			continue
		}

		// 跳过查询 key 对应的前缀段
		// 查询 "/ioa"(prefixSegCount=1)，key "/ioa/agent/config" segments=["ioa","agent","config"]
		// 跳过前 1 段 → 相对段 ["agent","config"]
		relSegs := segments[prefixSegCount:]
		if len(relSegs) == 0 {
			continue
		}

		// 沿路径逐层创建/查找节点
		current := root
		for i, seg := range relSegs {
			child, exists := current.children[seg]
			if !exists {
				// 构建此节点的完整 key
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

		// 最终节点：填入实际 key 的数据
		current.isRealKey = true
		current.value = string(kv.Value)
		current.ttl = c.getTTL(kv.Lease)
		current.createRevision = kv.CreateRevision
		current.modRevision = kv.ModRevision
		current.version = kv.Version
	}

	// 将 nodeEntry 树转为 Node 树
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
			// 虚拟目录（无子节点也无真实 key）
			node.Dir = true
			node.Nodes = []Node{}
		} else {
			// 叶子节点（实际 key，无子节点）
			node.Dir = false
		}

		return node
	}

	// 提取 root 的子节点作为 rootNode 的子节点
	rootNode.Nodes = make([]Node, 0, len(root.children))
	for _, name := range root.childOrder {
		if child, ok := root.children[name]; ok {
			rootNode.Nodes = append(rootNode.Nodes, convertEntry(child))
		}
	}

	// 如果 "/" 本身也是一个真实 key，把元信息带上
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
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	var opts []clientv3.OpOption

	// TTL 通过 Lease 实现
	if ttl > 0 {
		leaseResp, err := c.client.Grant(ctx, ttl)
		if err != nil {
			return nil, fmt.Errorf("create lease failed: %w", err)
		}
		opts = append(opts, clientv3.WithLease(leaseResp.ID))
	}

	_, err := c.client.Put(ctx, key, value, opts...)
	if err != nil {
		return nil, err
	}

	// 读回确认
	return c.Get(key)
}

// Delete 删除 key 或目录
func (c *ClientV3) Delete(key string, dir bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	// 先删除 key 本身
	_, err := c.client.Delete(ctx, key)
	if err != nil {
		return err
	}

	// 如果是目录，递归删除所有以 key+separator 为前缀的子 key
	if dir {
		prefix := key + c.separator
		if key == c.separator {
			prefix = c.separator
		}
		_, err = c.client.Delete(ctx, prefix, clientv3.WithPrefix())
		if err != nil {
			return err
		}
	}

	return nil
}

// getTTL 获取 lease 的剩余 TTL
func (c *ClientV3) getTTL(leaseID int64) int64 {
	if leaseID == 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	resp, err := c.client.Lease.TimeToLive(ctx, clientv3.LeaseID(leaseID))
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
