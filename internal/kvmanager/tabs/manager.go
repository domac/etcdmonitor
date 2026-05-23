package tabs

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"
	"etcdmonitor/internal/logger"
	etcdtls "etcdmonitor/internal/tls"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Manager 编排 Tab 解析、连接构造、探活、默认集群成员比对。
type Manager struct {
	repo      Repo
	km        KeyManager
	cfg       *config.Config
	healthMgr *health.Manager // 可为 nil（仅在测试中允许）

	memberMu        sync.Mutex
	memberCache     map[string]struct{} // 默认集群成员 clientURLs 集合
	memberCachedAt  time.Time
	memberCacheTTL  time.Duration
	memberDegraded  bool // 上次成员发现是否走了降级

	// 抑制 ⚠️ 抖动：连续 N 次探活失败才把 last_status 改写为 "error"。
	// 跨网络 / 慢链路场景下，单次 5–10 秒超时可能因丢包等偶发原因触发，
	// 但该 Tab 实际仍然可用——这里用 in-memory 计数器消除假报。
	failureMu      sync.Mutex
	failureCount   map[string]int // tabID → 连续失败次数
	failureThreshold int          // 触发"error"所需的最小连续失败次数
}

// NewManager 创建 Manager 实例。
func NewManager(repo Repo, km KeyManager, cfg *config.Config, healthMgr *health.Manager) *Manager {
	return &Manager{
		repo:             repo,
		km:               km,
		cfg:              cfg,
		healthMgr:        healthMgr,
		memberCache:      nil,
		memberCacheTTL:   30 * time.Second,
		failureCount:     make(map[string]int),
		failureThreshold: 2, // 连续 2 次失败才视为真不通
	}
}

// 错误码——前端 / handler 据此映射 HTTP 状态。
var (
	ErrInvalidScheme  = errors.New("KV_TAB_INVALID_SCHEME")
	ErrUnreachable    = errors.New("KV_TAB_UNREACHABLE")
	ErrAuthFailed     = errors.New("KV_TAB_AUTH_FAILED")
	ErrUnknownConn    = errors.New("KV_TAB_UNKNOWN_ERROR")
	ErrBelongsDefault = errors.New("KV_TAB_BELONGS_TO_DEFAULT")
)

// ConnectionConfig 描述一次到 etcd 集群的连接所需的全部参数。
type ConnectionConfig struct {
	Endpoints []string    // 至少 1 个
	Username  string      // 空字符串 = 匿名
	Password  string      // 已解密的明文密码
	TLS       *tls.Config // nil = 明文
	IsDefault bool        // true = 来自 cfg.Etcd.*（默认 Tab）
}

// Resolve 解析 tabID 为 ConnectionConfig。
//
// tabID == "" 或 "default" 时返回默认集群配置（不查 repo，user_id 不参与）；
// 其他 tabID 必须归属 userID，否则返 ErrTabNotFound。
//
// 解密密码失败（如 KEK 丢失）会返回错误——上层 handler 应据此提示用户重新输入。
func (m *Manager) Resolve(tabID string, userID int64) (*ConnectionConfig, error) {
	if tabID == "" || tabID == "default" {
		return m.defaultConnection()
	}

	tab, err := m.repo.Get(tabID, userID)
	if err != nil {
		return nil, err // ErrTabNotFound 透传
	}

	password, err := m.km.Decrypt(tab.PasswordCipher)
	if err != nil {
		return nil, fmt.Errorf("decrypt password for tab %s: %w", tabID, err)
	}

	return &ConnectionConfig{
		Endpoints: []string{tab.Endpoint},
		Username:  tab.Username,
		Password:  string(password),
		TLS:       tlsForEndpoint(tab.Endpoint),
		IsDefault: false,
	}, nil
}

// defaultConnection 构造默认集群 ConnectionConfig。
//
// 默认集群延续现有"完整 TLS 配置"路径（mTLS / CA / SNI 都生效），
// 与 health.Manager / kvmanager.ClientV3 的行为完全一致。
func (m *Manager) defaultConnection() (*ConnectionConfig, error) {
	tlsCfg, err := etcdtls.LoadClientTLSConfig(m.cfg)
	if err != nil {
		return nil, fmt.Errorf("load default TLS config: %w", err)
	}

	endpoints := m.cfg.EtcdEndpoints()
	if m.healthMgr != nil {
		// 优先用健康端点（与 kvmanager.ClientV3.newClient 行为一致）
		if healthy := m.healthMgr.HealthyEndpoints(); len(healthy) > 0 {
			endpoints = healthy
		}
	}

	return &ConnectionConfig{
		Endpoints: endpoints,
		Username:  m.cfg.Etcd.Username,
		Password:  m.cfg.Etcd.Password,
		TLS:       tlsCfg,
		IsDefault: true,
	}, nil
}

// tlsForEndpoint 按 D10 决策：scheme 派生 TLS。
//
// https:// → InsecureSkipVerify=true（不校验证书）
// http://  → nil（明文）
// 其他 scheme → 由 validateScheme 在更早阶段拦截，理论上不会进到这里
func tlsForEndpoint(endpoint string) *tls.Config {
	if strings.HasPrefix(strings.ToLower(endpoint), "https://") {
		logger.Infof("[KV-Tabs] endpoint %q uses HTTPS with InsecureSkipVerify=true (no certificate verification)",
			endpoint)
		return &tls.Config{InsecureSkipVerify: true} // #nosec G402 — 用户明确决策，文档中已显式提示
	}
	return nil
}

// validateScheme 校验 endpoint 是否仅以 http:// 或 https:// 开头。
func (m *Manager) validateScheme(endpoint string) error {
	low := strings.ToLower(strings.TrimSpace(endpoint))
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		// 进一步校验能解析为有效 URL
		if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
			return nil
		}
	}
	return ErrInvalidScheme
}

// BuildClientV3 用 ConnectionConfig 构造一个临时 v3 client。调用方负责 defer Close()。
func (m *Manager) BuildClientV3(connCfg *ConnectionConfig) (*clientv3.Client, error) {
	dialTimeout := time.Duration(m.cfg.KVManager.ConnectTimeout) * time.Second
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	etcdCfg := clientv3.Config{
		Endpoints:   connCfg.Endpoints,
		DialTimeout: dialTimeout,
	}
	if connCfg.Username != "" {
		etcdCfg.Username = connCfg.Username
		etcdCfg.Password = connCfg.Password
	}
	if connCfg.TLS != nil {
		etcdCfg.TLS = connCfg.TLS
	}
	return clientv3.New(etcdCfg)
}

// Ping 探活：建临时 v3 client 调 Status()，把结果写回 last_status。
//
// 返回 (status, errMsg)：
//   status: "ok" / "unreachable" / "auth_failed" / "unknown_error"
//   errMsg: 详细错误（status="ok" 时为 ""）
//
// 不带 userID——探活与会话身份正交（D11）。
func (m *Manager) Ping(tabID string) (string, string) {
	// Ping 不走 Resolve（Resolve 需要 userID）；直接遍历 ListAll 找到 Tab
	all, err := m.repo.ListAll()
	if err != nil {
		return "unknown_error", fmt.Sprintf("list tabs: %v", err)
	}
	var target *Tab
	for i := range all {
		if all[i].ID == tabID {
			target = &all[i]
			break
		}
	}
	if target == nil {
		// Tab 已被删——不写回（UpdateStatus 也会 no-op，但提早 return 避免无谓的 etcd 连接）
		return "unknown_error", "tab not found"
	}

	password, err := m.km.Decrypt(target.PasswordCipher)
	if err != nil {
		// KEK 丢失等——记录但不写"error"，避免覆盖用户尚未来得及修复的状态
		logger.Warnf("[KV-Tabs] Ping(%s) decrypt failed: %v", tabID, err)
		return "unknown_error", err.Error()
	}

	connCfg := &ConnectionConfig{
		Endpoints: []string{target.Endpoint},
		Username:  target.Username,
		Password:  string(password),
		TLS:       tlsForEndpoint(target.Endpoint),
	}

	// 用 10 秒：跨网络 / 慢链路下 5 秒仍可能触发抖动。Add 流程也允许这个上限，
	// 业务 KV 请求 RequestTimeout 默认 30 秒——保留充足余量。
	status, errMsg := m.testConnectionRaw(connCfg, 10*time.Second)
	checkedAt := time.Now().Unix()

	// 连续失败计数：< 阈值时不写"error"，避免单次抖动覆盖正常 Tab 的状态。
	if status == "ok" {
		m.resetFailureCount(tabID)
		if err := m.repo.UpdateStatus(tabID, "ok", "", checkedAt); err != nil {
			logger.Warnf("[KV-Tabs] Ping(%s) UpdateStatus failed: %v", tabID, err)
		}
		return status, errMsg
	}

	count := m.bumpFailureCount(tabID)
	if count < m.failureThreshold {
		// 仅刷新 last_checked_at，保留原 last_status；不广播 ⚠️
		logger.Infof("[KV-Tabs] Ping(%s) failed (%d/%d, suppressed): %s",
			tabID, count, m.failureThreshold, errMsg)
		if err := m.repo.UpdateStatus(tabID, target.LastStatus, target.LastError, checkedAt); err != nil {
			logger.Warnf("[KV-Tabs] Ping(%s) UpdateStatus(suppressed) failed: %v", tabID, err)
		}
		return status, errMsg
	}

	// 达到阈值——正式标记 error
	storedErr := fmt.Sprintf("%s: %s", status, errMsg)
	if err := m.repo.UpdateStatus(tabID, "error", storedErr, checkedAt); err != nil {
		logger.Warnf("[KV-Tabs] Ping(%s) UpdateStatus failed: %v", tabID, err)
	}
	return status, errMsg
}

// resetFailureCount 清零给定 tab 的连续失败计数。Ping 成功 / MarkStatus("ok") 时调用。
func (m *Manager) resetFailureCount(tabID string) {
	m.failureMu.Lock()
	defer m.failureMu.Unlock()
	delete(m.failureCount, tabID)
}

// bumpFailureCount +1 并返回当前值。
func (m *Manager) bumpFailureCount(tabID string) int {
	m.failureMu.Lock()
	defer m.failureMu.Unlock()
	m.failureCount[tabID]++
	return m.failureCount[tabID]
}

// MarkStatus 由 KV 业务请求失败路径调用（不在探活循环里）。
//
// status="ok" 会同时清零连续失败计数——确保业务请求成功能立刻把 ⚠️ 摘掉。
func (m *Manager) MarkStatus(tabID, status, errMsg string) {
	if tabID == "" || tabID == "default" {
		return
	}
	if status == "ok" {
		m.resetFailureCount(tabID)
	}
	if err := m.repo.UpdateStatus(tabID, status, errMsg, time.Now().Unix()); err != nil {
		logger.Warnf("[KV-Tabs] MarkStatus(%s) failed: %v", tabID, err)
	}
}

// TestConnection 同步测试一组凭据（用于"添加 Tab"与"重连测试"对话框）。
//
// 5 秒硬超时；返回 (status, errMsg) 分类同 Ping。
func (m *Manager) TestConnection(connCfg *ConnectionConfig) (string, string) {
	return m.testConnectionRaw(connCfg, 5*time.Second)
}

// testConnectionRaw Ping 与 TestConnection 的公共实现。
func (m *Manager) testConnectionRaw(connCfg *ConnectionConfig, timeout time.Duration) (string, string) {
	dialTimeout := time.Duration(m.cfg.KVManager.ConnectTimeout) * time.Second
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	if dialTimeout > timeout {
		dialTimeout = timeout
	}

	etcdCfg := clientv3.Config{
		Endpoints:   connCfg.Endpoints,
		DialTimeout: dialTimeout,
	}
	if connCfg.Username != "" {
		etcdCfg.Username = connCfg.Username
		etcdCfg.Password = connCfg.Password
	}
	if connCfg.TLS != nil {
		etcdCfg.TLS = connCfg.TLS
	}

	cli, err := clientv3.New(etcdCfg)
	if err != nil {
		return classifyConnError(err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if len(connCfg.Endpoints) == 0 {
		return "unknown_error", "no endpoints"
	}
	_, err = cli.Status(ctx, connCfg.Endpoints[0])
	if err != nil {
		return classifyConnError(err)
	}
	return "ok", ""
}

// classifyConnError 把 etcd SDK 错误归类为 unreachable / auth_failed / unknown_error。
func classifyConnError(err error) (string, string) {
	if err == nil {
		return "ok", ""
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "authentication") ||
		strings.Contains(low, "etcdserver: invalid auth") ||
		strings.Contains(low, "user name is empty") ||
		strings.Contains(low, "auth failed") ||
		strings.Contains(low, "permission denied"):
		return "auth_failed", msg
	case strings.Contains(low, "context deadline") ||
		strings.Contains(low, "connection refused") ||
		strings.Contains(low, "no such host") ||
		strings.Contains(low, "no route to host") ||
		strings.Contains(low, "i/o timeout") ||
		strings.Contains(low, "transport: error while dialing") ||
		strings.Contains(low, "unavailable"):
		return "unreachable", msg
	default:
		return "unknown_error", msg
	}
}

// IsDefaultClusterMember 判断给定 endpoint 是否属于默认集群成员。
//
// 返回 (matched, degraded, err)：
//   matched: 命中默认集群成员（包含 matched_member_url）
//   degraded: 默认集群 MemberList 不可用，已降级为字符串比对 cfg.Etcd.Endpoint
//   err: 仅在两种比对都无法完成时（理论上几乎不会触发）
//
// 内部缓存默认集群成员列表 30 秒。
func (m *Manager) IsDefaultClusterMember(endpoint string) (matched bool, matchedURL string, degraded bool, err error) {
	members, deg, err := m.getDefaultMembers()
	if err != nil {
		return false, "", true, err
	}

	target := normalizeForMemberMatch(endpoint)
	for url := range members {
		if normalizeForMemberMatch(url) == target {
			return true, url, deg, nil
		}
	}
	return false, "", deg, nil
}

// getDefaultMembers 取（或刷新）默认集群成员缓存。
//
// 失败时降级：返回 cfg.Etcd.Endpoint 切分后的列表，degraded=true。
func (m *Manager) getDefaultMembers() (map[string]struct{}, bool, error) {
	m.memberMu.Lock()
	defer m.memberMu.Unlock()

	if m.memberCache != nil && time.Since(m.memberCachedAt) < m.memberCacheTTL {
		return m.memberCache, m.memberDegraded, nil
	}

	// 尝试用默认集群跑一次 MemberList
	connCfg, err := m.defaultConnection()
	if err != nil {
		return m.fallbackMembers(), true, nil
	}

	cli, err := m.BuildClientV3(connCfg)
	if err != nil {
		return m.fallbackMembers(), true, nil
	}
	defer cli.Close()

	timeout := time.Duration(m.cfg.KVManager.RequestTimeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := cli.MemberList(ctx)
	if err != nil {
		logger.Warnf("[KV-Tabs] MemberList failed, degrading to config endpoints: %v", err)
		fb := m.fallbackMembers()
		m.memberCache = fb
		m.memberCachedAt = time.Now()
		m.memberDegraded = true
		return fb, true, nil
	}

	urls := make(map[string]struct{})
	for _, mb := range resp.Members {
		for _, u := range mb.ClientURLs {
			urls[u] = struct{}{}
		}
	}
	m.memberCache = urls
	m.memberCachedAt = time.Now()
	m.memberDegraded = false
	return urls, false, nil
}

// fallbackMembers 降级——把 cfg.Etcd.Endpoint 切分后的列表当成员。
func (m *Manager) fallbackMembers() map[string]struct{} {
	out := make(map[string]struct{})
	for _, ep := range m.cfg.EtcdEndpoints() {
		out[ep] = struct{}{}
	}
	return out
}

// normalizeForMemberMatch 标准化 URL 用于比对（去尾斜杠、小写 scheme）。
//
// 注意：不做 DNS 解析——按字符串比对，DNS 别名 / IP 互转的边界 case 文档已说明。
func normalizeForMemberMatch(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "/")
	low := strings.ToLower(s)
	for _, scheme := range []string{"http://", "https://"} {
		if strings.HasPrefix(low, scheme) {
			return scheme + s[len(scheme):]
		}
	}
	return s
}
