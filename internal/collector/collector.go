package collector

import (
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/storage"
)

// Collector 负责从 etcd /metrics 端点采集 Prometheus 格式的指标
type Collector struct {
	cfg     *config.Config
	store   *storage.Storage
	client  *http.Client

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
func New(cfg *config.Config, store *storage.Storage) *Collector {
	return &Collector{
		cfg:   cfg,
		store: store,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		latest: make(map[string]map[string]float64),
		prev:   make(map[string]map[string]float64),
		prevTs: make(map[string]time.Time),
		stop:   make(chan struct{}),
	}
}

// Start 启动定时采集
func (c *Collector) Start() {
	interval := time.Duration(c.cfg.Collector.Interval) * time.Second
	log.Printf("[Collector] Starting, interval=%v, endpoint=%s", interval, c.cfg.Etcd.Endpoint)

	c.collect()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.collect()
		case <-c.stop:
			log.Println("[Collector] Stopped")
			return
		}
	}
}

// Stop 停止采集（可安全多次调用）
func (c *Collector) Stop() {
	c.stopOnce.Do(func() {
		close(c.stop)
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
				log.Printf("[Collector] Member discovery failed, keeping previous member list")
				c.mu.Lock()
				c.lastMemberSync = time.Now()
				c.mu.Unlock()
			} else {
				log.Printf("[Collector] No members discovered on first run, falling back to config endpoint")
				c.mu.Lock()
				c.members = []MemberInfo{{
					ID:         "default",
					Name:       "default",
					ClientURLs: []string{c.cfg.Etcd.Endpoint},
					Endpoint:   c.cfg.Etcd.Endpoint,
					IsDefault:  true,
				}}
				c.lastMemberSync = time.Now()
				c.mu.Unlock()
				log.Printf("[Collector] Member sync completed: 1 member (fallback)")
			}
		} else {
			for _, m := range members {
				log.Printf("[Collector] Member: id=%s name=%s endpoint=%s isDefault=%v", m.ID, m.Name, m.Endpoint, m.IsDefault)
			}

			c.mu.Lock()
			c.members = members
			c.lastMemberSync = time.Now()
			c.mu.Unlock()

			log.Printf("[Collector] Member sync completed: %d members", len(members))
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
	for res := range results {
		if err := c.store.Store(now, res.member.ID, res.snapshot); err != nil {
			log.Printf("[Collector] [%s] Error storing metrics: %v", res.member.Name, err)
		} else {
			log.Printf("[Collector] [%s] Collected %d metrics", res.member.Name, len(res.snapshot))
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
		log.Printf("[Collector] [%s] Error creating request: %v", member.Name, err)
		return nil
	}

	if c.cfg.Etcd.Username != "" {
		req.SetBasicAuth(c.cfg.Etcd.Username, c.cfg.Etcd.Password)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		log.Printf("[Collector] [%s] Error fetching metrics from %s: %v", member.Name, metricsURL, err)
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
				log.Printf("[Collector] [%s] Error creating unauthenticated request: %v", member.Name, err)
				return nil
			}
			resp2, err := c.client.Do(req2)
			if err != nil {
				log.Printf("[Collector] [%s] Unauthenticated fallback failed: %v", member.Name, err)
				return nil
			}
			defer resp2.Body.Close()
			if resp2.StatusCode == http.StatusOK {
				log.Printf("[Collector] [%s] Metrics endpoint accessible without auth", member.Name)
				return c.parseMetrics(resp2.Body, member, now)
			}
			log.Printf("[Collector] [%s] Auth failed, unauthenticated fallback also returned %d", member.Name, resp2.StatusCode)
			return nil
		}
		log.Printf("[Collector] [%s] Unexpected status code: %d", member.Name, resp.StatusCode)
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

		case family.Name == "etcd_disk_wal_fsync_duration_seconds":
			ExtractHistogram(family, "etcd_disk_wal_fsync_duration_seconds", snapshot)
		case family.Name == "etcd_disk_backend_commit_duration_seconds":
			ExtractHistogram(family, "etcd_disk_backend_commit_duration_seconds", snapshot)

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

		case family.Name == "grpc_server_handled_total":
			ExtractGRPCMetrics(family, snapshot)
		case family.Name == "grpc_server_started_total":
			snapshot["grpc_server_started_total"] = SumValues(family)

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
