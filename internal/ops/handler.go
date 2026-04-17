package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"etcdmonitor/internal/auth"
	"etcdmonitor/internal/collector"
	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"
	"etcdmonitor/internal/logger"
	"etcdmonitor/internal/storage"
	"etcdmonitor/internal/tls"

	"github.com/gin-gonic/gin"
	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// OpsHandler 运维操作 HTTP 处理器
type OpsHandler struct {
	cfg          *config.Config
	store        *storage.Storage
	collector    *collector.Collector
	healthMgr    *health.Manager
	sessionStore *auth.MemorySessionStore
	authRequired bool
}

// New 创建 OpsHandler 实例
func New(cfg *config.Config, store *storage.Storage, c *collector.Collector, healthMgr *health.Manager, sessionStore *auth.MemorySessionStore, authRequired bool) *OpsHandler {
	return &OpsHandler{
		cfg:          cfg,
		store:        store,
		collector:    c,
		healthMgr:    healthMgr,
		sessionStore: sessionStore,
		authRequired: authRequired,
	}
}

// RegisterRoutes 注册 /ops/* 路由到受保护路由组
func (h *OpsHandler) RegisterRoutes(group *gin.RouterGroup) {
	ops := group.Group("/ops")
	ops.Use(h.opsEnableMiddleware())
	{
		ops.POST("/defragment", h.handleDefragment)
		ops.GET("/snapshot", snapshotTimeoutMiddleware(), h.handleSnapshot)
		ops.GET("/alarms", h.handleAlarmList)
		ops.POST("/alarms/disarm", h.handleAlarmDisarm)
		ops.POST("/move-leader", h.handleMoveLeader)
		ops.POST("/hashkv", h.handleHashKV)
		ops.POST("/compact", h.handleCompact)
		ops.GET("/compact/revision", h.handleCompactRevision)
		ops.GET("/audit-logs", h.handleAuditLogs)
	}
}

// opsEnableMiddleware 检查 ops_enable 配置，禁用时返回 403
func (h *OpsHandler) opsEnableMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.cfg.OpsEnabled() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "ops panel is disabled"})
			return
		}
		c.Next()
	}
}

// snapshotTimeoutMiddleware 使用 ResponseController 延长 snapshot 下载的写超时
func snapshotTimeoutMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rc := http.NewResponseController(c.Writer)
		if err := rc.SetWriteDeadline(time.Now().Add(30 * time.Minute)); err != nil {
			logger.Warnf("[Ops] Failed to set write deadline for snapshot: %v", err)
		}
		c.Next()
	}
}

// newEtcdClient 创建临时 etcd 客户端
func (h *OpsHandler) newEtcdClient() (*clientv3.Client, error) {
	return h.newEtcdClientWithEndpoints(h.healthMgr.HealthyEndpoints())
}

// newEtcdClientWithEndpoints 使用指定端点创建 etcd 客户端
func (h *OpsHandler) newEtcdClientWithEndpoints(endpoints []string) (*clientv3.Client, error) {
	etcdCfg := clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	}
	if h.cfg.Etcd.Username != "" {
		etcdCfg.Username = h.cfg.Etcd.Username
		etcdCfg.Password = h.cfg.Etcd.Password
	}
	tlsCfg, err := tls.LoadClientTLSConfig(h.cfg)
	if err != nil {
		logger.Errorf("[Ops] Failed to load TLS configuration: %v", err)
	} else if tlsCfg != nil {
		etcdCfg.TLS = tlsCfg
	}
	return clientv3.New(etcdCfg)
}

// getUsername 从请求中提取当前用户名
func (h *OpsHandler) getUsername(c *gin.Context) string {
	if !h.authRequired {
		return "anonymous"
	}
	token := auth.ExtractToken(c.Request)
	if token == "" {
		return "anonymous"
	}
	session := h.sessionStore.Get(token)
	if session == nil {
		return "anonymous"
	}
	return session.Username
}

// logAudit 记录审计日志，写入失败不影响运维操作
func (h *OpsHandler) logAudit(username, operation, target, params, result string, durationMs int64, success bool) {
	entry := storage.AuditEntry{
		Timestamp:  time.Now().Unix(),
		Username:   username,
		Operation:  operation,
		Target:     target,
		Params:     params,
		Result:     result,
		DurationMs: durationMs,
		Success:    success,
	}
	if err := h.store.StoreAuditLog(entry); err != nil {
		logger.Errorf("[Ops] Failed to write audit log: %v (operation=%s target=%s)", err, operation, target)
	}
}

// resolveEndpoint 根据 member_id 查找该成员的 endpoint 地址
func (h *OpsHandler) resolveEndpoint(memberID string) (string, string) {
	members := h.collector.GetMembers()
	for _, m := range members {
		if m.ID == memberID {
			ep := m.Endpoint
			if ep == "" && len(m.ClientURLs) > 0 {
				ep = m.ClientURLs[0]
			}
			return ep, m.Name
		}
	}
	return "", ""
}

// handleDefragment 对单个成员执行碎片整理
func (h *OpsHandler) handleDefragment(c *gin.Context) {
	var req struct {
		MemberID string `json:"member_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.MemberID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "member_id is required"})
		return
	}

	username := h.getUsername(c)
	endpoint, memberName := h.resolveEndpoint(req.MemberID)
	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "member not found"})
		return
	}

	logger.Infof("[Ops] Defragment started: member=%s(%s) endpoint=%s by=%s", memberName, req.MemberID, endpoint, username)

	cli, err := h.newEtcdClient()
	if err != nil {
		errMsg := fmt.Sprintf("create etcd client: %v", err)
		logger.Errorf("[Ops] Defragment failed: %s", errMsg)
		h.logAudit(username, "defragment", memberName, req.MemberID, errMsg, 0, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}
	defer cli.Close()

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_, err = cli.Defragment(ctx, endpoint)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		errMsg := fmt.Sprintf("defragment failed: %v", err)
		logger.Warnf("[Ops] Defragment failed: member=%s duration=%dms error=%v", memberName, durationMs, err)
		h.logAudit(username, "defragment", memberName, req.MemberID, errMsg, durationMs, false)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":       errMsg,
			"member_id":   req.MemberID,
			"member_name": memberName,
			"duration_ms": durationMs,
		})
		return
	}

	logger.Infof("[Ops] Defragment completed: member=%s duration=%dms", memberName, durationMs)
	h.logAudit(username, "defragment", memberName, req.MemberID, "success", durationMs, true)
	c.JSON(http.StatusOK, gin.H{
		"message":     "defragment completed",
		"member_id":   req.MemberID,
		"member_name": memberName,
		"duration_ms": durationMs,
	})
}

// handleSnapshot 流式下载集群快照
func (h *OpsHandler) handleSnapshot(c *gin.Context) {
	memberID := c.Query("member_id")
	if memberID == "" {
		memberID = h.collector.GetDefaultMemberID()
	}

	username := h.getUsername(c)
	_, memberName := h.resolveEndpoint(memberID)
	if memberName == "" {
		memberName = memberID
	}

	logger.Infof("[Ops] Snapshot started: member=%s by=%s", memberName, username)

	cli, err := h.newEtcdClient()
	if err != nil {
		errMsg := fmt.Sprintf("create etcd client: %v", err)
		logger.Errorf("[Ops] Snapshot failed: %s", errMsg)
		h.logAudit(username, "snapshot", memberName, memberID, errMsg, 0, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}
	defer cli.Close()

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	reader, err := cli.Snapshot(ctx)
	if err != nil {
		durationMs := time.Since(start).Milliseconds()
		errMsg := fmt.Sprintf("snapshot failed: %v", err)
		logger.Warnf("[Ops] Snapshot failed: member=%s error=%v", memberName, err)
		h.logAudit(username, "snapshot", memberName, memberID, errMsg, durationMs, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}
	defer reader.Close()

	filename := fmt.Sprintf("etcd-snapshot-%s-%s.db", memberName, time.Now().Format("20060102-150405"))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Type", "application/octet-stream")

	written, err := io.Copy(c.Writer, reader)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		errMsg := fmt.Sprintf("snapshot transfer failed after %d bytes: %v", written, err)
		logger.Warnf("[Ops] Snapshot transfer failed: member=%s written=%d error=%v", memberName, written, err)
		h.logAudit(username, "snapshot", memberName, memberID, errMsg, durationMs, false)
		return
	}

	result := fmt.Sprintf("success, size=%d bytes", written)
	logger.Infof("[Ops] Snapshot completed: member=%s size=%d duration=%dms", memberName, written, durationMs)
	h.logAudit(username, "snapshot", memberName, memberID, result, durationMs, true)
}

// handleAlarmList 查询当前集群告警
func (h *OpsHandler) handleAlarmList(c *gin.Context) {
	cli, err := h.newEtcdClient()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create etcd client: %v", err)})
		return
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := cli.AlarmList(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("alarm list failed: %v", err)})
		return
	}

	// 将成员 ID 映射到名称
	members := h.collector.GetMembers()
	memberMap := make(map[uint64]string)
	for _, m := range members {
		if id, err := strconv.ParseUint(m.ID, 10, 64); err == nil {
			memberMap[id] = m.Name
		}
	}

	type alarmItem struct {
		MemberID   string `json:"member_id"`
		MemberName string `json:"member_name"`
		AlarmType  string `json:"alarm_type"`
	}

	var alarms []alarmItem
	for _, a := range resp.Alarms {
		name := memberMap[a.MemberID]
		if name == "" {
			name = fmt.Sprintf("%d", a.MemberID)
		}
		alarms = append(alarms, alarmItem{
			MemberID:   fmt.Sprintf("%d", a.MemberID),
			MemberName: name,
			AlarmType:  a.Alarm.String(),
		})
	}

	c.JSON(http.StatusOK, gin.H{"alarms": alarms})
}

// handleAlarmDisarm 解除告警
func (h *OpsHandler) handleAlarmDisarm(c *gin.Context) {
	var req struct {
		MemberID  string `json:"member_id"`
		AlarmType string `json:"alarm_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	username := h.getUsername(c)
	_, memberName := h.resolveEndpoint(req.MemberID)
	if memberName == "" {
		memberName = req.MemberID
	}

	logger.Infof("[Ops] Alarm disarm started: member=%s type=%s by=%s", memberName, req.AlarmType, username)

	cli, err := h.newEtcdClient()
	if err != nil {
		errMsg := fmt.Sprintf("create etcd client: %v", err)
		h.logAudit(username, "alarm_disarm", memberName, fmt.Sprintf("type=%s", req.AlarmType), errMsg, 0, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}
	defer cli.Close()

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	memberIDUint, _ := strconv.ParseUint(req.MemberID, 10, 64)

	// 解析告警类型
	var alarmType pb.AlarmType
	switch req.AlarmType {
	case "NOSPACE", "AlarmType_NOSPACE":
		alarmType = pb.AlarmType_NOSPACE
	case "CORRUPT", "AlarmType_CORRUPT":
		alarmType = pb.AlarmType_CORRUPT
	default:
		alarmType = pb.AlarmType_NOSPACE
	}

	_, err = cli.AlarmDisarm(ctx, &clientv3.AlarmMember{
		MemberID: memberIDUint,
		Alarm:    alarmType,
	})
	durationMs := time.Since(start).Milliseconds()

	params := fmt.Sprintf("member_id=%s, alarm_type=%s", req.MemberID, req.AlarmType)
	if err != nil {
		errMsg := fmt.Sprintf("alarm disarm failed: %v", err)
		logger.Warnf("[Ops] Alarm disarm failed: member=%s error=%v", memberName, err)
		h.logAudit(username, "alarm_disarm", memberName, params, errMsg, durationMs, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	logger.Infof("[Ops] Alarm disarm completed: member=%s type=%s duration=%dms", memberName, req.AlarmType, durationMs)
	h.logAudit(username, "alarm_disarm", memberName, params, "success", durationMs, true)
	c.JSON(http.StatusOK, gin.H{"message": "alarm disarmed"})
}

// handleMoveLeader 迁移 Leader 到目标成员
func (h *OpsHandler) handleMoveLeader(c *gin.Context) {
	var req struct {
		TargetMemberID string `json:"target_member_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TargetMemberID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_member_id is required"})
		return
	}

	username := h.getUsername(c)
	_, targetName := h.resolveEndpoint(req.TargetMemberID)
	if targetName == "" {
		targetName = req.TargetMemberID
	}

	logger.Infof("[Ops] MoveLeader started: target=%s(%s) by=%s", targetName, req.TargetMemberID, username)

	targetID, err := strconv.ParseUint(req.TargetMemberID, 10, 64)
	if err != nil {
		errMsg := fmt.Sprintf("invalid member_id: %v", err)
		h.logAudit(username, "move_leader", targetName, req.TargetMemberID, errMsg, 0, false)
		c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
		return
	}

	// MoveLeader must be called on the current leader node.
	// 1) Get leader ID from any healthy endpoint via Status.
	// 2) Get leader's client URL from MemberList.
	// 3) Create a client connected only to the leader.
	cli, err := h.newEtcdClient()
	if err != nil {
		errMsg := fmt.Sprintf("create etcd client: %v", err)
		h.logAudit(username, "move_leader", targetName, req.TargetMemberID, errMsg, 0, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	findCtx, findCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer findCancel()

	// Get leader ID from any endpoint's status
	var leaderID uint64
	endpoints := h.healthMgr.HealthyEndpoints()
	for _, ep := range endpoints {
		resp, statusErr := cli.Status(findCtx, ep)
		if statusErr == nil && resp.Leader != 0 {
			leaderID = resp.Leader
			break
		}
	}
	if leaderID == 0 {
		cli.Close()
		errMsg := "cannot determine leader from cluster status"
		h.logAudit(username, "move_leader", targetName, req.TargetMemberID, errMsg, 0, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	// Find leader's client URL via MemberList
	membersResp, err := cli.MemberList(findCtx)
	cli.Close()
	if err != nil {
		errMsg := fmt.Sprintf("list members: %v", err)
		h.logAudit(username, "move_leader", targetName, req.TargetMemberID, errMsg, 0, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	var leaderEndpoints []string
	for _, m := range membersResp.Members {
		if m.ID == leaderID {
			leaderEndpoints = m.ClientURLs
			break
		}
	}
	if len(leaderEndpoints) == 0 {
		errMsg := fmt.Sprintf("leader %d has no client URLs", leaderID)
		h.logAudit(username, "move_leader", targetName, req.TargetMemberID, errMsg, 0, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	leaderCli, err := h.newEtcdClientWithEndpoints(leaderEndpoints)
	if err != nil {
		errMsg := fmt.Sprintf("connect to leader: %v", err)
		h.logAudit(username, "move_leader", targetName, req.TargetMemberID, errMsg, 0, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}
	defer leaderCli.Close()

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = leaderCli.MoveLeader(ctx, targetID)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		errMsg := fmt.Sprintf("move leader failed: %v", err)
		logger.Warnf("[Ops] MoveLeader failed: target=%s error=%v", targetName, err)
		h.logAudit(username, "move_leader", targetName, req.TargetMemberID, errMsg, durationMs, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	logger.Infof("[Ops] MoveLeader completed: target=%s duration=%dms", targetName, durationMs)
	h.logAudit(username, "move_leader", targetName, req.TargetMemberID, "success", durationMs, true)
	c.JSON(http.StatusOK, gin.H{
		"message":     "leader moved",
		"target_id":   req.TargetMemberID,
		"target_name": targetName,
		"duration_ms": durationMs,
	})
}

// handleHashKV 对所有成员执行 HashKV 一致性校验
func (h *OpsHandler) handleHashKV(c *gin.Context) {
	username := h.getUsername(c)
	logger.Infof("[Ops] HashKV started by=%s", username)

	cli, err := h.newEtcdClient()
	if err != nil {
		errMsg := fmt.Sprintf("create etcd client: %v", err)
		h.logAudit(username, "hashkv", "cluster", "", errMsg, 0, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}
	defer cli.Close()

	members := h.collector.GetMembers()
	if len(members) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no members available"})
		return
	}

	// 先获取统一的 revision
	start := time.Now()
	statusCtx, statusCancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = statusCtx
	statusResp, err := h.healthMgr.StatusFromHealthy(cli)
	statusCancel()
	if err != nil {
		errMsg := fmt.Sprintf("get status failed: %v", err)
		h.logAudit(username, "hashkv", "cluster", "", errMsg, 0, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}
	revision := statusResp.Header.Revision

	type hashResult struct {
		MemberID   string `json:"member_id"`
		MemberName string `json:"member_name"`
		Hash       uint32 `json:"hash"`
		Revision   int64  `json:"revision"`
		Error      string `json:"error,omitempty"`
	}

	results := make([]hashResult, len(members))

	// 并发对所有成员执行 HashKV
	type indexedResult struct {
		idx int
		res hashResult
	}
	ch := make(chan indexedResult, len(members))

	for i, m := range members {
		go func(idx int, member collector.MemberInfo) {
			endpoint := member.Endpoint
			if endpoint == "" && len(member.ClientURLs) > 0 {
				endpoint = member.ClientURLs[0]
			}

			r := hashResult{
				MemberID:   member.ID,
				MemberName: member.Name,
				Revision:   revision,
			}

			if endpoint == "" {
				r.Error = "no endpoint"
				ch <- indexedResult{idx, r}
				return
			}

			hctx, hcancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer hcancel()

			resp, err := cli.HashKV(hctx, endpoint, revision)
			if err != nil {
				r.Error = err.Error()
			} else {
				r.Hash = resp.Hash
				r.Revision = resp.Header.Revision
			}
			ch <- indexedResult{idx, r}
		}(i, m)
	}

	for range members {
		ir := <-ch
		results[ir.idx] = ir.res
	}

	durationMs := time.Since(start).Milliseconds()

	// 判断一致性
	consistent := true
	var referenceHash uint32
	hasReference := false
	for _, r := range results {
		if r.Error != "" {
			continue
		}
		if !hasReference {
			referenceHash = r.Hash
			hasReference = true
		} else if r.Hash != referenceHash {
			consistent = false
			break
		}
	}

	resultJSON, _ := json.Marshal(results)
	resultSummary := "consistent"
	if !consistent {
		resultSummary = "INCONSISTENT"
	}

	logger.Infof("[Ops] HashKV completed: consistent=%v revision=%d duration=%dms", consistent, revision, durationMs)
	h.logAudit(username, "hashkv", "cluster", fmt.Sprintf("revision=%d", revision), resultSummary+", "+string(resultJSON), durationMs, consistent)

	c.JSON(http.StatusOK, gin.H{
		"consistent":  consistent,
		"revision":    revision,
		"results":     results,
		"duration_ms": durationMs,
	})
}

// handleCompact 执行集群级 compact 操作
func (h *OpsHandler) handleCompact(c *gin.Context) {
	var req struct {
		RetainCount int64 `json:"retain_count"`
		Physical    bool  `json:"physical"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.RetainCount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "retain_count must be a positive integer"})
		return
	}

	username := h.getUsername(c)

	logger.Infof("[Ops] Compact started: retain_count=%d physical=%v by=%s", req.RetainCount, req.Physical, username)

	cli, err := h.newEtcdClient()
	if err != nil {
		errMsg := fmt.Sprintf("create etcd client: %v", err)
		logger.Errorf("[Ops] Compact failed: %s", errMsg)
		h.logAudit(username, "compact", "cluster",
			fmt.Sprintf(`{"retain_count":%d,"physical":%v}`, req.RetainCount, req.Physical),
			errMsg, 0, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}
	defer cli.Close()

	// 获取当前 Revision
	statusResp, err := h.healthMgr.StatusFromHealthy(cli)
	if err != nil {
		errMsg := fmt.Sprintf("get cluster status: %v", err)
		logger.Errorf("[Ops] Compact failed: %s", errMsg)
		h.logAudit(username, "compact", "cluster",
			fmt.Sprintf(`{"retain_count":%d,"physical":%v}`, req.RetainCount, req.Physical),
			errMsg, 0, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	currentRevision := statusResp.Header.Revision
	targetRevision := currentRevision - req.RetainCount

	if targetRevision <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":            "retain_count is too large, computed target revision is non-positive",
			"current_revision": currentRevision,
			"retain_count":     req.RetainCount,
		})
		return
	}

	// 构建 compact 选项
	var opts []clientv3.CompactOption
	if req.Physical {
		opts = append(opts, clientv3.WithCompactPhysical())
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_, err = cli.Compact(ctx, targetRevision, opts...)
	durationMs := time.Since(start).Milliseconds()

	params := fmt.Sprintf(`{"retain_count":%d,"physical":%v,"target_revision":%d}`, req.RetainCount, req.Physical, targetRevision)

	if err != nil {
		errMsg := fmt.Sprintf("compact failed: %v", err)
		logger.Warnf("[Ops] Compact failed: duration=%dms error=%v", durationMs, err)
		h.logAudit(username, "compact", "cluster", params, errMsg, durationMs, false)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":       errMsg,
			"duration_ms": durationMs,
		})
		return
	}

	logger.Infof("[Ops] Compact completed: current_revision=%d target_revision=%d physical=%v duration=%dms",
		currentRevision, targetRevision, req.Physical, durationMs)
	h.logAudit(username, "compact", "cluster", params, "success", durationMs, true)
	c.JSON(http.StatusOK, gin.H{
		"message":          "compact completed",
		"current_revision": currentRevision,
		"target_revision":  targetRevision,
		"retain_count":     req.RetainCount,
		"physical":         req.Physical,
		"duration_ms":      durationMs,
	})
}

// handleCompactRevision 返回当前集群 Revision，供前端面板展示
func (h *OpsHandler) handleCompactRevision(c *gin.Context) {
	cli, err := h.newEtcdClient()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create etcd client: %v", err)})
		return
	}
	defer cli.Close()

	statusResp, err := h.healthMgr.StatusFromHealthy(cli)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("get cluster status: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"revision": statusResp.Header.Revision,
	})
}

// handleAuditLogs 查询审计日志
func (h *OpsHandler) handleAuditLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	operation := c.Query("operation")

	filter := storage.AuditFilter{
		Operation: operation,
		Page:      page,
		PageSize:  pageSize,
	}

	entries, total, err := h.store.QueryAuditLogs(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("query audit logs: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"entries":   entries,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
