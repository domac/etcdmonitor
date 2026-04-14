package health

import (
	"context"
	"crypto/tls"
	"sync"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/logger"
	etcdtls "etcdmonitor/internal/tls"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	probeTimeout      = 3 * time.Second
	checkInterval     = 15 * time.Second
	statusCallTimeout = 3 * time.Second
)

// Manager 全局健康端点管理器
// 启动时探测所有配置地址，后台定时健康检查，
// 恢复的地址自动加回，全部不可用时退出程序
type Manager struct {
	cfg          *config.Config
	allEndpoints []string // 配置的全部地址（不变）
	tlsCfg       *tls.Config // 缓存 TLS 配置

	mu      sync.RWMutex
	healthy []string // 当前可用地址，保持 config 原始顺序

	stop     chan struct{}
	stopOnce sync.Once
}

// New 创建健康端点管理器，并行探测所有地址
// 至少一个可用才返回成功，全部不可用返回 error
func New(cfg *config.Config) (*Manager, error) {
	allEps := cfg.EtcdEndpoints()

	// 加载 TLS 配置
	tlsCfg, err := etcdtls.LoadClientTLSConfig(cfg)
	if err != nil {
		logger.Errorf("[Health] Failed to load TLS configuration: %v", err)
		return nil, err
	}

	m := &Manager{
		cfg:          cfg,
		allEndpoints: allEps,
		tlsCfg:       tlsCfg,
		stop:         make(chan struct{}),
	}

	// 启动时并行探测所有地址
	healthy := m.probeAll()

	if len(healthy) == 0 {
		logger.Errorf("[Health] All endpoints unreachable: %v", allEps)
		return nil, &AllEndpointsDownError{Endpoints: allEps}
	}

	m.healthy = healthy

	logger.Infof("[Health] Initial probe: %d/%d endpoints healthy: %v", len(healthy), len(allEps), healthy)
	if len(healthy) < len(allEps) {
		for _, ep := range allEps {
			if !contains(healthy, ep) {
				logger.Warnf("[Health] Endpoint unhealthy at startup: %s", ep)
			}
		}
	}

	return m, nil
}

// HealthyEndpoints 返回当前可用地址列表的副本
func (m *Manager) HealthyEndpoints() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]string, len(m.healthy))
	copy(result, m.healthy)
	return result
}

// StatusFromHealthy 按健康列表顺序尝试 Status()，返回第一个成功的结果
func (m *Manager) StatusFromHealthy(client *clientv3.Client) (*clientv3.StatusResponse, error) {
	eps := m.HealthyEndpoints()

	var lastErr error
	for _, ep := range eps {
		ctx, cancel := context.WithTimeout(context.Background(), statusCallTimeout)
		resp, err := client.Status(ctx, ep)
		cancel()
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	// 健康列表全部失败，尝试所有地址（可能刚好有恢复的）
	for _, ep := range m.allEndpoints {
		if contains(eps, ep) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), statusCallTimeout)
		resp, err := client.Status(ctx, ep)
		cancel()
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, &AllEndpointsDownError{Endpoints: m.allEndpoints}
}

// StartBackgroundCheck 启动后台健康检查 goroutine
func (m *Manager) StartBackgroundCheck() {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.runHealthCheck()
		case <-m.stop:
			logger.Info("[Health] Background check stopped")
			return
		}
	}
}

// Close 停止后台健康检查
func (m *Manager) Close() {
	m.stopOnce.Do(func() {
		close(m.stop)
	})
}

// runHealthCheck 执行一轮健康检查，更新可用列表
func (m *Manager) runHealthCheck() {
	newHealthy := m.probeAll()

	m.mu.RLock()
	oldHealthy := make([]string, len(m.healthy))
	copy(oldHealthy, m.healthy)
	m.mu.RUnlock()

	// 检测状态变化并 log
	for _, ep := range m.allEndpoints {
		wasHealthy := contains(oldHealthy, ep)
		isHealthy := contains(newHealthy, ep)

		if !wasHealthy && isHealthy {
			logger.Infof("[Health] Endpoint recovered: %s", ep)
		} else if wasHealthy && !isHealthy {
			logger.Warnf("[Health] Endpoint became unhealthy: %s", ep)
		}
	}

	m.mu.Lock()
	m.healthy = newHealthy
	m.mu.Unlock()

	// 全部不可用 → Fatal 退出
	if len(newHealthy) == 0 {
		logger.Fatalf("[Health] All endpoints unreachable, shutting down: %v", m.allEndpoints)
	}
}

// probeAll 并行探测所有端点，返回可用的（保持 config 原始顺序）
func (m *Manager) probeAll() []string {
	type probeResult struct {
		index   int
		healthy bool
	}

	results := make(chan probeResult, len(m.allEndpoints))
	var wg sync.WaitGroup

	for i, ep := range m.allEndpoints {
		wg.Add(1)
		go func(idx int, endpoint string) {
			defer wg.Done()
			ok := m.probeEndpoint(endpoint)
			results <- probeResult{index: idx, healthy: ok}
		}(i, ep)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// 收集结果
	healthyMap := make(map[int]bool)
	for r := range results {
		if r.healthy {
			healthyMap[r.index] = true
		}
	}

	// 按 config 原始顺序返回
	var healthy []string
	for i, ep := range m.allEndpoints {
		if healthyMap[i] {
			healthy = append(healthy, ep)
		}
	}
	return healthy
}

// probeEndpoint 探测单个端点是否可用
func (m *Manager) probeEndpoint(endpoint string) bool {
	etcdCfg := clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: probeTimeout,
	}
	if m.cfg.Etcd.Username != "" {
		etcdCfg.Username = m.cfg.Etcd.Username
		etcdCfg.Password = m.cfg.Etcd.Password
	}

	// 应用 TLS 配置
	if m.tlsCfg != nil {
		etcdCfg.TLS = m.tlsCfg
	}

	cli, err := clientv3.New(etcdCfg)
	if err != nil {
		return false
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	_, err = cli.Status(ctx, endpoint)
	return err == nil
}

// AllEndpointsDownError 全部端点不可用错误
type AllEndpointsDownError struct {
	Endpoints []string
}

func (e *AllEndpointsDownError) Error() string {
	return "all etcd endpoints unreachable"
}

// contains 检查切片中是否包含指定字符串
func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
