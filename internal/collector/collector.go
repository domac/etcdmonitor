package collector

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"
	"etcdmonitor/internal/logger"
	"etcdmonitor/internal/storage"
	"etcdmonitor/internal/tls"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Collector 负责从 etcd /metrics 端点采集 Prometheus 格式的指标
type Collector struct {
	cfg        *config.Config
	store      *storage.Storage
	client     *http.Client
	etcdClient *clientv3.Client
	healthMgr  *health.Manager

	mu             sync.RWMutex
	members        []MemberInfo
	latest         map[string]map[string]float64 // member_id -> metrics
	prev           map[string]map[string]float64 // member_id -> prev metrics
	prevTs         map[string]time.Time          // member_id -> prev time
	lastMemberSync time.Time

	stop     chan struct{}
	stopOnce sync.Once
}

// New 创建采集器实例
func New(cfg *config.Config, store *storage.Storage, healthMgr *health.Manager) *Collector {
	// 创建 etcd v3 SDK 客户端用于成员发现
	etcdCfg := clientv3.Config{
		Endpoints:   cfg.EtcdEndpoints(),
		DialTimeout: 5 * time.Second,
	}
	if cfg.Etcd.Username != "" {
		etcdCfg.Username = cfg.Etcd.Username
		etcdCfg.Password = cfg.Etcd.Password
	}

	// 应用 TLS 配置
	tlsCfg, err := tls.LoadClientTLSConfig(cfg)
	if err != nil {
		logger.Errorf("[Collector] Failed to load TLS configuration: %v", err)
	} else if tlsCfg != nil {
		etcdCfg.TLS = tlsCfg
	}

	etcdClient, err := clientv3.New(etcdCfg)
	if err != nil {
		logger.Errorf("[Collector] Failed to create etcd SDK client: %v, member discovery may fail", err)
	}

	// 创建 HTTP 客户端，用于采集 /metrics（如果启用了 TLS，需要携带客户端证书）
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}
	if tlsCfg != nil {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: tlsCfg,
		}
		logger.Info("[Collector] HTTP client configured with TLS for /metrics collection")
	}

	return &Collector{
		cfg:        cfg,
		store:      store,
		etcdClient: etcdClient,
		healthMgr:  healthMgr,
		client:     httpClient,
		latest: make(map[string]map[string]float64),
		prev:   make(map[string]map[string]float64),
		prevTs: make(map[string]time.Time),
		stop:   make(chan struct{}),
	}
}

// Start 启动定时采集
func (c *Collector) Start() {
	interval := time.Duration(c.cfg.Collector.Interval) * time.Second
	logger.Infof("[Collector] Starting, interval=%v, endpoints=%v", interval, c.cfg.EtcdEndpoints())

	c.collect()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.collect()
		case <-c.stop:
			logger.Info("[Collector] Stopped")
			return
		}
	}
}

// Stop 停止采集（可安全多次调用）
func (c *Collector) Stop() {
	c.stopOnce.Do(func() {
		close(c.stop)
		if c.etcdClient != nil {
			c.etcdClient.Close()
		}
	})
}

// GetLatest 获取指定成员的最新指标快照
func (c *Collector) GetLatest(memberID string) map[string]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	snapshot, ok := c.latest[memberID]
	if !ok {
		return nil
	}
	result := make(map[string]float64, len(snapshot))
	for k, v := range snapshot {
		result[k] = v
	}
	return result
}

// GetMembers 返回当前所有成员信息
func (c *Collector) GetMembers() []MemberInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]MemberInfo, len(c.members))
	copy(result, c.members)

	// 根据 latest snapshot 填充 IsLeader
	for i := range result {
		if latest, ok := c.latest[result[i].ID]; ok {
			result[i].IsLeader = latest["etcd_server_is_leader"] == 1
		}
	}

	return result
}

// GetDefaultMemberID 获取 config.yaml 配置节点对应的 member ID
func (c *Collector) GetDefaultMemberID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, m := range c.members {
		if m.IsDefault {
			return m.ID
		}
	}
	if len(c.members) > 0 {
		return c.members[0].ID
	}
	return ""
}

// GetVersion 获取 etcd 集群版本号
func (c *Collector) GetVersion() string {
	if c.etcdClient == nil {
		return ""
	}
	resp := c.statusFromAnyEndpoint()
	if resp == nil {
		return ""
	}
	return resp.Version
}

// statusFromAnyEndpoint 通过健康管理器获取 Status
func (c *Collector) statusFromAnyEndpoint() *clientv3.StatusResponse {
	if c.etcdClient == nil {
		return nil
	}
	resp, err := c.healthMgr.StatusFromHealthy(c.etcdClient)
	if err != nil {
		logger.Warnf("[Collector] StatusFromHealthy failed: %v", err)
		return nil
	}
	return resp
}

// injectRaftStatus 对单个成员调用 Status() 获取 raft_term / raft_index 并注入 snapshot
func (c *Collector) injectRaftStatus(member MemberInfo, snapshot map[string]float64) {
	if c.etcdClient == nil || snapshot == nil {
		return
	}

	endpoint := member.CollectEndpoint
	if endpoint == "" {
		endpoint = member.Endpoint
	}
	if endpoint == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.etcdClient.Status(ctx, endpoint)
	if err != nil {
		logger.Debugf("[Collector] [%s] Status() for raft info failed: %v", member.Name, err)
		return
	}

	snapshot["raft_term"] = float64(resp.RaftTerm)
	snapshot["raft_index"] = float64(resp.RaftIndex)
}

// injectLeaseCount 调用一次 Leases() 获取活跃 Lease 数量，注入所有成员 snapshot
func (c *Collector) injectLeaseCount(snapshots []memberSnapshot) {
	if c.etcdClient == nil || len(snapshots) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.etcdClient.Leases(ctx)
	if err != nil {
		logger.Warnf("[Collector] Leases() for lease count failed: %v", err)
		return
	}

	count := float64(len(resp.Leases))
	for i := range snapshots {
		snapshots[i].snapshot["etcd_lease_count"] = count
	}
}

// memberSnapshot 一次采集的结果
type memberSnapshot struct {
	member   MemberInfo
	snapshot map[string]float64
}

// collect 每 60 秒同步成员 + 并发采集所有成员
func (c *Collector) collect() {
	// 按需刷新成员列表
	c.mu.RLock()
	needSync := c.members == nil || time.Since(c.lastMemberSync) >= 60*time.Second
	c.mu.RUnlock()

	if needSync {
		members := c.discoverMembers()

		if len(members) == 0 {
			c.mu.RLock()
			hasExisting := len(c.members) > 0
			c.mu.RUnlock()

			if hasExisting {
				logger.Warnf("[Collector] Member discovery failed, keeping previous member list")
				c.mu.Lock()
				c.lastMemberSync = time.Now()
				c.mu.Unlock()
			} else {
				logger.Warnf("[Collector] No members discovered on first run, falling back to config endpoint")
				c.mu.Lock()
				c.members = []MemberInfo{{
					ID:         "default",
					Name:       "default",
					ClientURLs: c.cfg.EtcdEndpoints(),
					Endpoint:   c.cfg.EtcdFirstEndpoint(),
					IsDefault:  true,
				}}
				c.lastMemberSync = time.Now()
				c.mu.Unlock()
				logger.Infof("[Collector] Member sync completed: 1 member (fallback)")
			}
		} else {
			for _, m := range members {
				logger.Infof("[Collector] Member: id=%s name=%s endpoint=%s isDefault=%v", m.ID, m.Name, m.Endpoint, m.IsDefault)
			}

			c.mu.Lock()
			c.members = members
			c.lastMemberSync = time.Now()
			c.mu.Unlock()

			logger.Infof("[Collector] Member sync completed: %d members", len(members))
		}
	}

	// 获取当前成员列表快照
	c.mu.RLock()
	members := make([]MemberInfo, len(c.members))
	copy(members, c.members)
	c.mu.RUnlock()

	// 阶段1：并发采集所有成员的 metrics（只读 HTTP，不写 SQLite）
	now := time.Now()
	results := make(chan memberSnapshot, len(members))
	var wg sync.WaitGroup

	for _, member := range members {
		wg.Add(1)
		go func(m MemberInfo) {
			defer wg.Done()
			snapshot := c.fetchMemberMetrics(m, now)
			if snapshot != nil {
				results <- memberSnapshot{member: m, snapshot: snapshot}
			}
		}(member)
	}

	// 等待所有采集完成后关闭 channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// 阶段2：串行写入 SQLite（避免 SQLITE_BUSY）
	// 写入前注入 raft_term / raft_index（通过 Status() gRPC 获取）
	var collected []memberSnapshot
	for res := range results {
		c.injectRaftStatus(res.member, res.snapshot)
		collected = append(collected, res)
	}

	// 注入 Lease 总数（集群级，调用一次注入所有成员）
	c.injectLeaseCount(collected)

	// 写入 SQLite
	for _, res := range collected {
		if err := c.store.Store(now, res.member.ID, res.snapshot); err != nil {
			logger.Errorf("[Collector] [%s] Error storing metrics: %v", res.member.Name, err)
		} else {
			logger.Infof("[Collector] [%s] Collected %d metrics", res.member.Name, len(res.snapshot))
		}
	}
}

// fetchMemberMetrics 并发安全：采集单个成员的 metrics 到内存，不写 SQLite
func (c *Collector) fetchMemberMetrics(member MemberInfo, now time.Time) map[string]float64 {
	collectURL := member.CollectEndpoint
	if collectURL == "" {
		collectURL = member.Endpoint
	}
	metricsURL := collectURL + c.cfg.Etcd.MetricsPath

	req, err := http.NewRequest("GET", metricsURL, nil)
	if err != nil {
		logger.Errorf("[Collector] [%s] Error creating request: %v", member.Name, err)
		return nil
	}

	if c.cfg.Etcd.Username != "" {
		req.SetBasicAuth(c.cfg.Etcd.Username, c.cfg.Etcd.Password)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		logger.Errorf("[Collector] [%s] Error fetching metrics from %s: %v", member.Name, metricsURL, err)
		c.mu.Lock()
		if c.latest[member.ID] == nil {
			c.latest[member.ID] = make(map[string]float64)
		}
		c.latest[member.ID]["collector_up"] = 0
		c.mu.Unlock()
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// If auth returned 401, try without auth (some etcd configs expose /metrics without auth)
		if resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			req2, err := http.NewRequest("GET", metricsURL, nil)
			if err != nil {
				logger.Errorf("[Collector] [%s] Error creating unauthenticated request: %v", member.Name, err)
				return nil
			}
			resp2, err := c.client.Do(req2)
			if err != nil {
				logger.Warnf("[Collector] [%s] Unauthenticated fallback failed: %v", member.Name, err)
				return nil
			}
			defer resp2.Body.Close()
			if resp2.StatusCode == http.StatusOK {
				logger.Infof("[Collector] [%s] Metrics endpoint accessible without auth", member.Name)
				return c.parseMetrics(resp2.Body, member, now)
			}
			logger.Warnf("[Collector] [%s] Auth failed, unauthenticated fallback also returned %d", member.Name, resp2.StatusCode)
			return nil
		}
		logger.Warnf("[Collector] [%s] Unexpected status code: %d", member.Name, resp.StatusCode)
		return nil
	}

	return c.parseMetrics(resp.Body, member, now)
}

// parseMetrics 解析 metrics 并更新内存状态，返回 snapshot（不写 SQLite）
func (c *Collector) parseMetrics(body io.Reader, member MemberInfo, now time.Time) map[string]float64 {
	families := ParsePrometheusText(body)
	snapshot := make(map[string]float64)
	snapshot["collector_up"] = 1

	for _, family := range families {
		switch {
		case family.Name == "etcd_server_has_leader":
			snapshot["etcd_server_has_leader"] = FirstValue(family)
		case family.Name == "etcd_server_is_leader":
			snapshot["etcd_server_is_leader"] = FirstValue(family)
		case family.Name == "etcd_server_leader_changes_seen_total":
			snapshot["etcd_server_leader_changes_seen_total"] = FirstValue(family)
		case family.Name == "etcd_server_proposals_committed_total":
			snapshot["etcd_server_proposals_committed_total"] = FirstValue(family)
		case family.Name == "etcd_server_proposals_applied_total":
			snapshot["etcd_server_proposals_applied_total"] = FirstValue(family)
		case family.Name == "etcd_server_proposals_pending":
			snapshot["etcd_server_proposals_pending"] = FirstValue(family)
		case family.Name == "etcd_server_proposals_failed_total":
			snapshot["etcd_server_proposals_failed_total"] = FirstValue(family)
		case family.Name == "etcd_server_slow_apply_total":
			snapshot["etcd_server_slow_apply_total"] = FirstValue(family)
		case family.Name == "etcd_server_slow_read_indexes_total":
			snapshot["etcd_server_slow_read_indexes_total"] = FirstValue(family)

		// === Server 扩展指标 ===
		case family.Name == "etcd_server_quota_backend_bytes":
			snapshot["etcd_server_quota_backend_bytes"] = FirstValue(family)
		case family.Name == "etcd_server_heartbeat_send_failures_total":
			snapshot["etcd_server_heartbeat_send_failures_total"] = FirstValue(family)
		case family.Name == "etcd_server_read_indexes_failed_total":
			snapshot["etcd_server_read_indexes_failed_total"] = FirstValue(family)
		case family.Name == "etcd_server_client_requests_total":
			ExtractClientRequests(family, snapshot)
		case family.Name == "etcd_server_health_failures":
			snapshot["etcd_server_health_failures"] = FirstValue(family)
		case family.Name == "etcd_server_health_success":
			snapshot["etcd_server_health_success"] = FirstValue(family)

		case family.Name == "etcd_disk_wal_fsync_duration_seconds":
			ExtractHistogram(family, "etcd_disk_wal_fsync_duration_seconds", snapshot)
		case family.Name == "etcd_disk_backend_commit_duration_seconds":
			ExtractHistogram(family, "etcd_disk_backend_commit_duration_seconds", snapshot)

		// === Disk 扩展指标 ===
		case family.Name == "etcd_disk_backend_defrag_duration_seconds":
			ExtractHistogram(family, "etcd_disk_backend_defrag_duration_seconds", snapshot)
		case family.Name == "etcd_disk_backend_snapshot_duration_seconds":
			ExtractHistogram(family, "etcd_disk_backend_snapshot_duration_seconds", snapshot)
		case family.Name == "etcd_disk_wal_write_bytes_total":
			snapshot["etcd_disk_wal_write_bytes_total"] = FirstValue(family)
		case family.Name == "etcd_snap_db_fsync_duration_seconds":
			ExtractHistogram(family, "etcd_snap_db_fsync_duration_seconds", snapshot)
		case family.Name == "etcd_snap_db_save_total_duration_seconds":
			ExtractHistogram(family, "etcd_snap_db_save_total_duration_seconds", snapshot)
		// Backend commit 子阶段
		case family.Name == "etcd_debugging_disk_backend_commit_rebalance_duration_seconds":
			ExtractHistogram(family, "etcd_disk_commit_rebalance_duration_seconds", snapshot)
		case family.Name == "etcd_debugging_disk_backend_commit_spill_duration_seconds":
			ExtractHistogram(family, "etcd_disk_commit_spill_duration_seconds", snapshot)
		case family.Name == "etcd_debugging_disk_backend_commit_write_duration_seconds":
			ExtractHistogram(family, "etcd_disk_commit_write_duration_seconds", snapshot)

		case family.Name == "etcd_mvcc_db_total_size_in_bytes":
			snapshot["etcd_mvcc_db_total_size_in_bytes"] = FirstValue(family)
		case family.Name == "etcd_mvcc_db_total_size_in_use_in_bytes":
			snapshot["etcd_mvcc_db_total_size_in_use_in_bytes"] = FirstValue(family)
		case family.Name == "etcd_debugging_mvcc_keys_total":
			snapshot["etcd_mvcc_keys_total"] = FirstValue(family)
		case family.Name == "etcd_mvcc_put_total":
			snapshot["etcd_mvcc_put_total"] = FirstValue(family)
		case family.Name == "etcd_mvcc_delete_total":
			snapshot["etcd_mvcc_delete_total"] = FirstValue(family)
		case family.Name == "etcd_mvcc_txn_total":
			snapshot["etcd_mvcc_txn_total"] = FirstValue(family)
		case family.Name == "etcd_mvcc_range_total":
			snapshot["etcd_mvcc_range_total"] = FirstValue(family)
		case family.Name == "etcd_debugging_mvcc_watch_stream_total":
			snapshot["etcd_mvcc_watch_stream_total"] = FirstValue(family)
		case family.Name == "etcd_debugging_mvcc_watcher_total":
			snapshot["etcd_mvcc_watcher_total"] = FirstValue(family)
		case family.Name == "etcd_debugging_mvcc_slow_watcher_total":
			snapshot["etcd_mvcc_slow_watcher_total"] = FirstValue(family)
		case family.Name == "etcd_debugging_mvcc_db_open_read_transactions":
			snapshot["etcd_mvcc_db_open_read_transactions"] = FirstValue(family)

		// === MVCC 扩展指标 ===
		case family.Name == "etcd_debugging_mvcc_compact_revision":
			snapshot["etcd_mvcc_compact_revision"] = FirstValue(family)
		case family.Name == "etcd_debugging_mvcc_current_revision":
			snapshot["etcd_mvcc_current_revision"] = FirstValue(family)
		case family.Name == "etcd_debugging_mvcc_events_total":
			snapshot["etcd_mvcc_events_total"] = FirstValue(family)
		case family.Name == "etcd_debugging_mvcc_pending_events_total":
			snapshot["etcd_mvcc_pending_events_total"] = FirstValue(family)
		case family.Name == "etcd_debugging_mvcc_total_put_size_in_bytes":
			snapshot["etcd_mvcc_total_put_size_in_bytes"] = FirstValue(family)
		case family.Name == "etcd_debugging_mvcc_db_compaction_keys_total":
			snapshot["etcd_mvcc_db_compaction_keys_total"] = FirstValue(family)
		case family.Name == "etcd_debugging_mvcc_db_compaction_pause_duration_milliseconds":
			ExtractHistogramMs(family, "etcd_mvcc_db_compaction_pause_duration", snapshot)
		case family.Name == "etcd_debugging_mvcc_db_compaction_total_duration_milliseconds":
			ExtractHistogramMs(family, "etcd_mvcc_db_compaction_total_duration", snapshot)
		case family.Name == "etcd_mvcc_hash_duration_seconds":
			ExtractHistogram(family, "etcd_mvcc_hash_duration_seconds", snapshot)
		case family.Name == "etcd_mvcc_hash_rev_duration_seconds":
			ExtractHistogram(family, "etcd_mvcc_hash_rev_duration_seconds", snapshot)

		case family.Name == "etcd_network_peer_sent_bytes_total":
			snapshot["etcd_network_peer_sent_bytes_total"] = SumValues(family)
		case family.Name == "etcd_network_peer_received_bytes_total":
			snapshot["etcd_network_peer_received_bytes_total"] = SumValues(family)
		case family.Name == "etcd_network_peer_sent_failures_total":
			snapshot["etcd_network_peer_sent_failures_total"] = SumValues(family)
		case family.Name == "etcd_network_peer_received_failures_total":
			snapshot["etcd_network_peer_received_failures_total"] = SumValues(family)
		case family.Name == "etcd_network_peer_round_trip_time_seconds":
			ExtractHistogram(family, "etcd_network_peer_round_trip_time_seconds", snapshot)
		case family.Name == "etcd_network_client_grpc_sent_bytes_total":
			snapshot["etcd_network_client_grpc_sent_bytes_total"] = FirstValue(family)
		case family.Name == "etcd_network_client_grpc_received_bytes_total":
			snapshot["etcd_network_client_grpc_received_bytes_total"] = FirstValue(family)

		// === Lease 指标 ===
		case family.Name == "etcd_debugging_lease_granted_total":
			snapshot["etcd_lease_granted_total"] = FirstValue(family)
		case family.Name == "etcd_debugging_lease_revoked_total":
			snapshot["etcd_lease_revoked_total"] = FirstValue(family)
		case family.Name == "etcd_debugging_lease_renewed_total":
			snapshot["etcd_lease_renewed_total"] = FirstValue(family)
		case family.Name == "etcd_debugging_server_lease_expired_total":
			snapshot["etcd_lease_expired_total"] = FirstValue(family)

		// === Network 扩展指标 ===
		case family.Name == "etcd_network_active_peers":
			snapshot["etcd_network_active_peers"] = FirstValue(family)

		case family.Name == "grpc_server_handled_total":
			ExtractGRPCMetrics(family, snapshot)
		case family.Name == "grpc_server_started_total":
			snapshot["grpc_server_started_total"] = SumValues(family)
		// === gRPC 扩展指标 ===
		case family.Name == "grpc_server_msg_received_total":
			snapshot["grpc_server_msg_received_total"] = SumValues(family)
		case family.Name == "grpc_server_msg_sent_total":
			snapshot["grpc_server_msg_sent_total"] = SumValues(family)

		case family.Name == "process_cpu_seconds_total":
			snapshot["process_cpu_seconds_total"] = FirstValue(family)
		case family.Name == "process_resident_memory_bytes":
			snapshot["process_resident_memory_bytes"] = FirstValue(family)
		case family.Name == "process_open_fds":
			snapshot["process_open_fds"] = FirstValue(family)
		case family.Name == "go_goroutines":
			snapshot["go_goroutines"] = FirstValue(family)
		case family.Name == "go_memstats_alloc_bytes":
			snapshot["go_memstats_alloc_bytes"] = FirstValue(family)
		case family.Name == "go_memstats_sys_bytes":
			snapshot["go_memstats_sys_bytes"] = FirstValue(family)
		case family.Name == "go_gc_duration_seconds":
			ExtractSummary(family, "go_gc_duration_seconds", snapshot)
		}
	}

	// 衍生指标
	committed := snapshot["etcd_server_proposals_committed_total"]
	applied := snapshot["etcd_server_proposals_applied_total"]
	if committed > 0 && applied > 0 {
		snapshot["etcd_server_proposals_commit_apply_lag"] = committed - applied
	}

	c.mu.RLock()
	prevSnapshot := c.prev[member.ID]
	prevTime := c.prevTs[member.ID]
	c.mu.RUnlock()

	if !prevTime.IsZero() && prevSnapshot != nil {
		elapsed := now.Sub(prevTime).Seconds()
		if elapsed > 0 {
			// Proposal Failed Rate
			prevFailed := prevSnapshot["etcd_server_proposals_failed_total"]
			curFailed := snapshot["etcd_server_proposals_failed_total"]
			if curFailed >= prevFailed {
				snapshot["etcd_server_proposals_failed_rate"] = (curFailed - prevFailed) / elapsed
			}

			// CPU Usage Rate（百分比）
			prevCPU := prevSnapshot["process_cpu_seconds_total"]
			curCPU := snapshot["process_cpu_seconds_total"]
			if curCPU >= prevCPU {
				snapshot["process_cpu_usage_percent"] = (curCPU - prevCPU) / elapsed * 100
			}
		}
	}

	c.mu.Lock()
	c.prev[member.ID] = c.latest[member.ID]
	c.prevTs[member.ID] = now
	c.latest[member.ID] = snapshot
	c.mu.Unlock()

	return snapshot
}
