package kvmanager

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"etcdmonitor/internal/config"

	clientv2 "go.etcd.io/etcd/client/v2"
)

// ClientV2 封装 etcd v2 客户端操作
type ClientV2 struct {
	keysAPI   clientv2.KeysAPI
	client    clientv2.Client
	cfg       *config.Config
	separator string
	available bool // v2 API 是否可用
}

// NewClientV2 创建 v2 客户端实例
func NewClientV2(cfg *config.Config) (*ClientV2, error) {
	c := &ClientV2{
		cfg:       cfg,
		separator: cfg.KVManager.Separator,
		available: false,
	}

	transport := clientv2.DefaultTransport

	etcdCfg := clientv2.Config{
		Endpoints:               []string{cfg.Etcd.Endpoint},
		Transport:               transport,
		HeaderTimeoutPerRequest: time.Duration(cfg.KVManager.ConnectTimeout) * time.Second,
	}
	if cfg.Etcd.Username != "" {
		etcdCfg.Username = cfg.Etcd.Username
		etcdCfg.Password = cfg.Etcd.Password
	}

	client, err := clientv2.New(etcdCfg)
	if err != nil {
		// v2 创建失败不阻断启动，标记为不可用
		return c, nil
	}

	c.client = client
	c.keysAPI = clientv2.NewKeysAPI(client)
	c.available = true

	return c, nil
}

// IsAvailable 返回 v2 API 是否可用
func (c *ClientV2) IsAvailable() bool {
	return c.available
}

// GetSeparator 返回路径分隔符
func (c *ClientV2) GetSeparator() string {
	return c.separator
}

// Connect 获取集群连接信息并检测 v2 是否可用
func (c *ClientV2) Connect() (*ConnectInfo, error) {
	if !c.available {
		return nil, fmt.Errorf("etcd v2 API is not available (requires --enable-v2=true)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	// 尝试获取根节点来验证 v2 API 可用
	_, err := c.keysAPI.Get(ctx, c.separator, &clientv2.GetOptions{Recursive: false})
	if err != nil {
		c.available = false
		return nil, fmt.Errorf("etcd v2 API is not available: %w", err)
	}

	// v2 没有像 v3 那样的 Status API，返回基本信息
	return &ConnectInfo{
		Version: "v2",
		Name:    "",
		Size:    0,
		SizeStr: "-",
	}, nil
}

// Get 获取单个 key 的值
func (c *ClientV2) Get(key string) (*Node, error) {
	if !c.available {
		return nil, fmt.Errorf("etcd v2 API is not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	resp, err := c.keysAPI.Get(ctx, key, nil)
	if err != nil {
		return nil, err
	}

	return c.nodeFromV2(resp.Node), nil
}

// GetPath 获取指定路径下的子节点（树结构）
func (c *ClientV2) GetPath(key string) (*Node, error) {
	if !c.available {
		return nil, fmt.Errorf("etcd v2 API is not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	resp, err := c.keysAPI.Get(ctx, key, &clientv2.GetOptions{
		Recursive: true,
		Sort:      true,
	})
	if err != nil {
		// 如果 key 不存在，返回空根节点
		if isV2KeyNotFound(err) {
			return &Node{
				Key:   key,
				Dir:   true,
				Nodes: []Node{},
			}, nil
		}
		return nil, err
	}

	node := c.nodeFromV2(resp.Node)
	return node, nil
}

// Put 创建或更新 key
func (c *ClientV2) Put(key, value string, ttl int64, dir bool) (*Node, error) {
	if !c.available {
		return nil, fmt.Errorf("etcd v2 API is not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	opts := &clientv2.SetOptions{}
	if ttl > 0 {
		opts.TTL = time.Duration(ttl) * time.Second
	}
	opts.Dir = dir

	var resp *clientv2.Response
	var err error

	if dir {
		resp, err = c.keysAPI.Set(ctx, key, "", opts)
	} else {
		resp, err = c.keysAPI.Set(ctx, key, value, opts)
	}
	if err != nil {
		return nil, err
	}

	return c.nodeFromV2(resp.Node), nil
}

// Delete 删除 key 或目录
func (c *ClientV2) Delete(key string, dir bool) error {
	if !c.available {
		return fmt.Errorf("etcd v2 API is not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	opts := &clientv2.DeleteOptions{
		Recursive: dir,
		Dir:       dir,
	}

	_, err := c.keysAPI.Delete(ctx, key, opts)
	return err
}

// nodeFromV2 将 v2 Node 转换为通用 Node 结构
func (c *ClientV2) nodeFromV2(n *clientv2.Node) *Node {
	if n == nil {
		return nil
	}

	node := &Node{
		Key:            n.Key,
		Value:          n.Value,
		Dir:            n.Dir,
		CreateRevision: int64(n.CreatedIndex),
		ModRevision:    int64(n.ModifiedIndex),
	}

	if n.TTL > 0 {
		node.TTL = n.TTL
	}

	if n.Dir && len(n.Nodes) > 0 {
		node.Nodes = make([]Node, 0, len(n.Nodes))
		// 排序子节点
		sort.Slice(n.Nodes, func(i, j int) bool {
			return n.Nodes[i].Key < n.Nodes[j].Key
		})
		for _, child := range n.Nodes {
			childNode := c.nodeFromV2(child)
			if childNode != nil {
				node.Nodes = append(node.Nodes, *childNode)
			}
		}
	} else if n.Dir {
		node.Nodes = []Node{}
	}

	return node
}

// isV2KeyNotFound 检查错误是否为 key 不存在
func isV2KeyNotFound(err error) bool {
	if err == nil {
		return false
	}
	if cErr, ok := err.(clientv2.Error); ok {
		return cErr.Code == clientv2.ErrorCodeKeyNotFound
	}
	return strings.Contains(err.Error(), "Key not found")
}

// requestTimeout 返回配置的请求超时时间
func (c *ClientV2) requestTimeout() time.Duration {
	return time.Duration(c.cfg.KVManager.RequestTimeout) * time.Second
}
