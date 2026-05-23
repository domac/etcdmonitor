package kvmanager

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"
	"etcdmonitor/internal/logger"
	etcdtls "etcdmonitor/internal/tls"

	clientv2 "go.etcd.io/etcd/client/v2"
)

// ClientV2 封装 etcd v2 客户端操作
type ClientV2 struct {
	keysAPI   clientv2.KeysAPI
	client    clientv2.Client
	cfg       *config.Config
	healthMgr *health.Manager
	separator string
	available bool // v2 API 是否可用

	// override 非 nil 时不持有 keysAPI；每次调用按 override 临时构造（per-Tab）
	override *connOverride
}

// NewClientV2 创建 v2 客户端实例（默认 Tab 行为，持有长 keysAPI）
func NewClientV2(cfg *config.Config, healthMgr *health.Manager) (*ClientV2, error) {
	c := &ClientV2{
		cfg:       cfg,
		healthMgr: healthMgr,
		separator: cfg.KVManager.Separator,
		available: false,
	}

	// 加载 TLS 配置并应用到 HTTP Transport
	tlsCfg, err := etcdtls.LoadClientTLSConfig(cfg)
	if err != nil {
		logger.Errorf("[KVManager] Failed to load TLS configuration: %v", err)
	}

	transport := newV2Transport(tlsCfg)

	etcdCfg := clientv2.Config{
		Endpoints:               healthMgr.HealthyEndpoints(),
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
		logger.Warnf("[KVManager] etcd v2 client creation failed: %v", err)
		return c, nil
	}

	c.client = client
	c.keysAPI = clientv2.NewKeysAPI(client)
	c.available = true

	return c, nil
}

// WithOverride 返回一个使用 per-Tab 凭据 / endpoints / TLS 的 ClientV2 副本。
//
// 副本不预构造 keysAPI——每次方法调用临时建 client + keysAPI（per-request）。
// 默认 Tab 与 per-Tab Tab 共用一个 ClientV2 类型，调用方无需感知差异。
func (c *ClientV2) WithOverride(endpoints []string, username, password string, tlsCfg *tls.Config) *ClientV2 {
	cp := &ClientV2{
		cfg:       c.cfg,
		healthMgr: c.healthMgr,
		separator: c.separator,
		available: true, // override 默认认为可达；不可达时方法内部 Connect 会探测
		override: &connOverride{
			Endpoints: endpoints,
			Username:  username,
			Password:  password,
			TLS:       tlsCfg,
		},
	}
	return cp
}

// keysClient 取 keysAPI——默认 Tab 用预构造的，per-Tab 临时构造。
func (c *ClientV2) keysClient() (clientv2.KeysAPI, error) {
	if c.override == nil {
		if !c.available || c.keysAPI == nil {
			return nil, fmt.Errorf("etcd v2 API is not available")
		}
		return c.keysAPI, nil
	}

	transport := newV2Transport(c.override.TLS)
	etcdCfg := clientv2.Config{
		Endpoints:               c.override.Endpoints,
		Transport:               transport,
		HeaderTimeoutPerRequest: time.Duration(c.cfg.KVManager.ConnectTimeout) * time.Second,
	}
	if c.override.Username != "" {
		etcdCfg.Username = c.override.Username
		etcdCfg.Password = c.override.Password
	}
	cli, err := clientv2.New(etcdCfg)
	if err != nil {
		return nil, fmt.Errorf("create v2 client: %w", err)
	}
	return clientv2.NewKeysAPI(cli), nil
}

// newV2Transport 构造带 TLS 的 HTTP Transport。
func newV2Transport(tlsCfg *tls.Config) *http.Transport {
	return &http.Transport{
		TLSClientConfig: tlsCfg,
	}
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
	api, err := c.keysClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	// 尝试获取根节点来验证 v2 API 可用
	_, err = api.Get(ctx, c.separator, &clientv2.GetOptions{Recursive: false})
	if err != nil {
		if c.override == nil {
			c.available = false
		}
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
	api, err := c.keysClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	resp, err := api.Get(ctx, key, nil)
	if err != nil {
		return nil, err
	}

	return c.nodeFromV2(resp.Node), nil
}

// GetPath 获取指定路径下的子节点（树结构）
func (c *ClientV2) GetPath(key string) (*Node, error) {
	api, err := c.keysClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	resp, err := api.Get(ctx, key, &clientv2.GetOptions{
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

// Keys 获取全量 key 树结构（不含 value），递归清空 value 字段
func (c *ClientV2) Keys() (*Node, error) {
	node, err := c.GetPath(c.separator)
	if err != nil {
		return nil, err
	}
	stripValues(node)
	return node, nil
}

// stripValues 递归清空节点树中所有的 value 字段
func stripValues(node *Node) {
	node.Value = ""
	for i := range node.Nodes {
		stripValues(&node.Nodes[i])
	}
}

// Put 创建或更新 key
func (c *ClientV2) Put(key, value string, ttl int64, dir bool) (*Node, error) {
	api, err := c.keysClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	opts := &clientv2.SetOptions{}
	if ttl > 0 {
		opts.TTL = time.Duration(ttl) * time.Second
	}
	opts.Dir = dir

	var resp *clientv2.Response

	if dir {
		resp, err = api.Set(ctx, key, "", opts)
	} else {
		resp, err = api.Set(ctx, key, value, opts)
	}
	if err != nil {
		return nil, err
	}

	return c.nodeFromV2(resp.Node), nil
}

// Delete 删除 key 或目录
func (c *ClientV2) Delete(key string, dir bool) error {
	api, err := c.keysClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout())
	defer cancel()

	opts := &clientv2.DeleteOptions{
		Recursive: dir,
		Dir:       dir,
	}

	_, err = api.Delete(ctx, key, opts)
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
