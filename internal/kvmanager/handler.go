package kvmanager

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"etcdmonitor/internal/auth"
	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"
	"etcdmonitor/internal/kvmanager/tabs"
	"etcdmonitor/internal/storage"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// KVHandler 提供 KV 管理的 HTTP 接口
type KVHandler struct {
	v3           *ClientV3
	v2           *ClientV2
	cfg          *config.Config
	healthMgr    *health.Manager
	logger       *zap.Logger
	store        *storage.Storage
	sessionStore *auth.MemorySessionStore
	authRequired bool

	// tabsMgr 提供按 tab_id 解析 ConnectionConfig 的能力。
	// 可为 nil（无多集群 Tab 支持的部署）；nil 时所有请求按默认集群处理。
	tabsMgr *tabs.Manager
	// tabsHandler 用于跨用户访问尝试时写审计；可为 nil。
	tabsHandler *tabs.TabHandler
}

// NewKVHandler 创建 KVHandler 实例
func NewKVHandler(cfg *config.Config, logger *zap.Logger, healthMgr *health.Manager,
	store *storage.Storage, sessionStore *auth.MemorySessionStore, authRequired bool) (*KVHandler, error) {
	v3, err := NewClientV3(cfg, healthMgr)
	if err != nil {
		return nil, fmt.Errorf("create v3 client: %w", err)
	}

	v2, err := NewClientV2(cfg, healthMgr)
	if err != nil {
		logger.Warn("v2 client creation failed, v2 API will be unavailable", zap.Error(err))
	}

	return &KVHandler{
		v3:           v3,
		v2:           v2,
		cfg:          cfg,
		healthMgr:    healthMgr,
		logger:       logger,
		store:        store,
		sessionStore: sessionStore,
		authRequired: authRequired,
	}, nil
}

// SetTabsManager 注入 tabs.Manager，启用按 tab_id 路由能力。
// 必须在 RegisterRoutes 前调用。
func (h *KVHandler) SetTabsManager(mgr *tabs.Manager, th *tabs.TabHandler) {
	h.tabsMgr = mgr
	h.tabsHandler = th
}

// Close 关闭所有客户端连接
func (h *KVHandler) Close() {
	if h.v3 != nil {
		h.v3.Close()
	}
}

// getUsername 从会话中获取用户名；到达此处的请求已经过 middleware 认证
func (h *KVHandler) getUsername(c *gin.Context) string {
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

// getUserID 从会话取 user_id（仅 per-Tab 路径需要）。
// 未登录或会话无 user_id 时返回 0。
func (h *KVHandler) getUserID(c *gin.Context) int64 {
	token := auth.ExtractToken(c.Request)
	if token == "" {
		return 0
	}
	session := h.sessionStore.Get(token)
	if session == nil {
		return 0
	}
	return session.UserID
}

// resolveClientV3 按 tab_id query 参数解析正确的 *ClientV3：
//   - tab_id == "" || "default" → 默认集群（h.v3 原样）
//   - tab_id != "default" → 调 tabsMgr.Resolve 拿 ConnectionConfig，
//     用 v3.WithOverride 派生新 ClientV3
//
// 返回 (client, tabID, error)。错误时已写 HTTP 响应；调用方直接 return。
// 注意：返回的 ClientV3 副本与 h.v3 共享 cfg/healthMgr/separator，可放心使用。
func (h *KVHandler) resolveClientV3(c *gin.Context) (*ClientV3, string, bool) {
	tabID := strings.TrimSpace(c.Query("tab_id"))
	if tabID == "" || tabID == "default" {
		return h.v3, "default", true
	}
	if h.tabsMgr == nil {
		// 部署未启用 tabs 支持但前端传了 tab_id → 视为不存在
		h.writeError(c, http.StatusNotFound, "multi-cluster tabs not enabled", "KV_TAB_NOT_FOUND")
		return nil, "", false
	}
	userID := h.getUserID(c)
	if userID <= 0 {
		h.writeError(c, http.StatusUnauthorized, "login required", "UNAUTHORIZED")
		return nil, "", false
	}
	connCfg, err := h.tabsMgr.Resolve(tabID, userID)
	if err != nil {
		if errors.Is(err, tabs.ErrTabNotFound) {
			// 跨用户 / 不存在统一 404；写跨用户审计
			if h.tabsHandler != nil {
				h.tabsHandler.LogCrossUserAttempt(h.getUsername(c), tabID, c.FullPath())
			}
			h.writeError(c, http.StatusNotFound, "tab not found", "KV_TAB_NOT_FOUND")
			return nil, "", false
		}
		// KEK 丢失等
		h.writeError(c, http.StatusInternalServerError, err.Error(), "INTERNAL")
		return nil, "", false
	}
	cli := h.v3.WithOverride(connCfg.Endpoints, connCfg.Username, connCfg.Password, connCfg.TLS)
	return cli, tabID, true
}

// resolveClientV2 V2 版本，逻辑同 resolveClientV3。
func (h *KVHandler) resolveClientV2(c *gin.Context) (*ClientV2, string, bool) {
	tabID := strings.TrimSpace(c.Query("tab_id"))
	if tabID == "" || tabID == "default" {
		return h.v2, "default", true
	}
	if h.tabsMgr == nil {
		h.writeError(c, http.StatusNotFound, "multi-cluster tabs not enabled", "KV_TAB_NOT_FOUND")
		return nil, "", false
	}
	userID := h.getUserID(c)
	if userID <= 0 {
		h.writeError(c, http.StatusUnauthorized, "login required", "UNAUTHORIZED")
		return nil, "", false
	}
	connCfg, err := h.tabsMgr.Resolve(tabID, userID)
	if err != nil {
		if errors.Is(err, tabs.ErrTabNotFound) {
			if h.tabsHandler != nil {
				h.tabsHandler.LogCrossUserAttempt(h.getUsername(c), tabID, c.FullPath())
			}
			h.writeError(c, http.StatusNotFound, "tab not found", "KV_TAB_NOT_FOUND")
			return nil, "", false
		}
		h.writeError(c, http.StatusInternalServerError, err.Error(), "INTERNAL")
		return nil, "", false
	}
	cli := h.v2.WithOverride(connCfg.Endpoints, connCfg.Username, connCfg.Password, connCfg.TLS)
	return cli, tabID, true
}

// markTabError 在 KV 业务请求失败时写回 last_status / last_error（仅非默认 Tab）。
func (h *KVHandler) markTabError(tabID string, err error) {
	if tabID == "" || tabID == "default" || h.tabsMgr == nil {
		return
	}
	h.tabsMgr.MarkStatus(tabID, "error", err.Error())
}

// markTabOK 在 KV 业务请求成功时清零 last_status——这样：
//   - 即使后台 ping 因抖动暂时失败，活跃 Tab 也能立刻摘掉 ⚠️
//   - 同时清零 Manager 内的连续失败计数（见 MarkStatus 实现）
//
// 仅对非默认 Tab 生效（默认 Tab 由 health.Manager 监控，不走此路径）。
func (h *KVHandler) markTabOK(tabID string) {
	if tabID == "" || tabID == "default" || h.tabsMgr == nil {
		return
	}
	h.tabsMgr.MarkStatus(tabID, "ok", "")
}

// logAudit 写入审计日志（非阻塞）
func (h *KVHandler) logAudit(username, operation, target, params, result string,
	durationMs int64, success bool) {
	if h.store == nil {
		return
	}
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
		h.logger.Error("[KV] Failed to write audit log",
			zap.String("operation", operation),
			zap.String("target", target),
			zap.Error(err))
	}
}

// RegisterRoutes 注册 KV 管理路由到 Gin 路由组
func (h *KVHandler) RegisterRoutes(rg *gin.RouterGroup) {
	// V3 路由
	v3 := rg.Group("/kv/v3")
	{
		v3.POST("/connect", h.handleV3Connect)
		v3.GET("/connect", h.handleV3Connect)
		v3.GET("/get", h.handleV3Get)
		v3.GET("/getpath", h.handleV3GetPath)
		v3.PUT("/put", h.handleV3Put)
		v3.POST("/delete", h.handleV3Delete)
		v3.GET("/separator", h.handleV3Separator)
		v3.GET("/keys", h.handleV3Keys)
	}

	// V2 路由
	v2 := rg.Group("/kv/v2")
	{
		v2.POST("/connect", h.handleV2Connect)
		v2.GET("/connect", h.handleV2Connect)
		v2.GET("/get", h.handleV2Get)
		v2.GET("/getpath", h.handleV2GetPath)
		v2.PUT("/put", h.handleV2Put)
		v2.POST("/delete", h.handleV2Delete)
		v2.GET("/separator", h.handleV2Separator)
		v2.GET("/keys", h.handleV2Keys)
	}
}

// ===== V3 Handlers =====

func (h *KVHandler) handleV3Connect(c *gin.Context) {
	v3, tabID, ok := h.resolveClientV3(c)
	if !ok {
		return
	}
	info, err := v3.Connect()
	if err != nil {
		h.logger.Error("v3 connect failed", zap.Error(err), zap.String("tab_id", tabID))
		h.markTabError(tabID, err)
		h.writeErrorFromEtcd(c, err)
		return
	}

	// 成功 connect 即视为 Tab 活跃——清零失败计数，立刻摘掉可能的 ⚠️。
	// 这是 Tab 切换时的高频钩子（kvRefreshInfoBar），覆盖大多数活跃场景。
	h.markTabOK(tabID)
	c.JSON(http.StatusOK, info)
}

func (h *KVHandler) handleV3Get(c *gin.Context) {
	v3, tabID, ok := h.resolveClientV3(c)
	if !ok {
		return
	}
	key := c.Query("key")
	if key == "" {
		h.writeError(c, http.StatusBadRequest, "key is required", "")
		return
	}

	node, err := v3.Get(key)
	if err != nil {
		h.markTabError(tabID, err)
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV3GetPath(c *gin.Context) {
	v3, tabID, ok := h.resolveClientV3(c)
	if !ok {
		return
	}
	key := c.Query("key")
	if key == "" {
		key = v3.GetSeparator()
	}

	node, err := v3.GetPath(key)
	if err != nil {
		h.markTabError(tabID, err)
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV3Put(c *gin.Context) {
	v3, tabID, ok := h.resolveClientV3(c)
	if !ok {
		return
	}
	var req PutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, http.StatusBadRequest, "invalid request body", "")
		return
	}

	username := h.getUsername(c)
	start := time.Now()
	paramsJSON := fmt.Sprintf(`{"ttl": %d, "tab_id": %q}`, req.TTL, tabID)
	auditTarget := req.Key
	if tabID != "default" {
		auditTarget = fmt.Sprintf("[tab=%s] %s", tabID, req.Key)
	}

	node, err := v3.Put(req.Key, req.Value, req.TTL)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		h.markTabError(tabID, err)
		h.logAudit(username, "put", auditTarget, paramsJSON, err.Error(), durationMs, false)
		h.writeErrorFromEtcd(c, err)
		return
	}

	h.logAudit(username, "put", auditTarget, paramsJSON, "ok", durationMs, true)
	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV3Delete(c *gin.Context) {
	v3, tabID, ok := h.resolveClientV3(c)
	if !ok {
		return
	}
	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, http.StatusBadRequest, "invalid request body", "")
		return
	}

	username := h.getUsername(c)
	start := time.Now()
	paramsJSON := fmt.Sprintf(`{"dir": %v, "tab_id": %q}`, req.Dir, tabID)
	auditTarget := req.Key
	if tabID != "default" {
		auditTarget = fmt.Sprintf("[tab=%s] %s", tabID, req.Key)
	}

	err := v3.Delete(req.Key, req.Dir)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		h.markTabError(tabID, err)
		h.logAudit(username, "delete", auditTarget, paramsJSON, err.Error(), durationMs, false)
		h.writeErrorFromEtcd(c, err)
		return
	}

	h.logAudit(username, "delete", auditTarget, paramsJSON, "ok", durationMs, true)
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *KVHandler) handleV3Separator(c *gin.Context) {
	v3, _, ok := h.resolveClientV3(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, SeparatorResponse{Separator: v3.GetSeparator()})
}

func (h *KVHandler) handleV3Keys(c *gin.Context) {
	v3, tabID, ok := h.resolveClientV3(c)
	if !ok {
		return
	}
	node, err := v3.Keys()
	if err != nil {
		h.markTabError(tabID, err)
		h.writeErrorFromEtcd(c, err)
		return
	}
	h.markTabOK(tabID)
	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

// ===== V2 Handlers =====

func (h *KVHandler) handleV2Connect(c *gin.Context) {
	v2, tabID, ok := h.resolveClientV2(c)
	if !ok {
		return
	}
	if v2 == nil {
		h.writeError(c, http.StatusServiceUnavailable, "etcd v2 API is not available (requires --enable-v2=true)", "v2_unavailable")
		return
	}

	info, err := v2.Connect()
	if err != nil {
		h.logger.Error("v2 connect failed", zap.Error(err), zap.String("tab_id", tabID))
		h.markTabError(tabID, err)
		h.writeError(c, http.StatusServiceUnavailable, err.Error(), "v2_unavailable")
		return
	}

	// 成功 connect 即视为 Tab 活跃——见 handleV3Connect 注释
	h.markTabOK(tabID)

	// V2 协议没有 Status API，借用 V3 客户端获取集群级信息（Version/Leader/DBSize）
	if v3, _, vok := h.resolveClientV3(c); vok && v3 != nil {
		if v3Info, vErr := v3.Connect(); vErr == nil {
			info.Version = v3Info.Version
			info.Name = v3Info.Name
			info.Size = v3Info.Size
			info.SizeStr = v3Info.SizeStr
		}
	}

	c.JSON(http.StatusOK, info)
}

func (h *KVHandler) handleV2Get(c *gin.Context) {
	v2, tabID, ok := h.resolveClientV2(c)
	if !ok {
		return
	}
	if !h.checkV2AvailableClient(c, v2) {
		return
	}

	key := c.Query("key")
	if key == "" {
		h.writeError(c, http.StatusBadRequest, "key is required", "")
		return
	}

	node, err := v2.Get(key)
	if err != nil {
		h.markTabError(tabID, err)
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV2GetPath(c *gin.Context) {
	v2, tabID, ok := h.resolveClientV2(c)
	if !ok {
		return
	}
	if !h.checkV2AvailableClient(c, v2) {
		return
	}

	key := c.Query("key")
	if key == "" {
		key = v2.GetSeparator()
	}

	node, err := v2.GetPath(key)
	if err != nil {
		h.markTabError(tabID, err)
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV2Put(c *gin.Context) {
	v2, tabID, ok := h.resolveClientV2(c)
	if !ok {
		return
	}
	if !h.checkV2AvailableClient(c, v2) {
		return
	}

	var req PutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, http.StatusBadRequest, "invalid request body", "")
		return
	}

	username := h.getUsername(c)
	start := time.Now()
	paramsJSON := fmt.Sprintf(`{"ttl": %d, "dir": %v, "tab_id": %q}`, req.TTL, req.Dir, tabID)
	auditTarget := req.Key
	if tabID != "default" {
		auditTarget = fmt.Sprintf("[tab=%s] %s", tabID, req.Key)
	}

	node, err := v2.Put(req.Key, req.Value, req.TTL, req.Dir)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		h.markTabError(tabID, err)
		h.logAudit(username, "put", auditTarget, paramsJSON, err.Error(), durationMs, false)
		h.writeErrorFromEtcd(c, err)
		return
	}

	h.logAudit(username, "put", auditTarget, paramsJSON, "ok", durationMs, true)
	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV2Delete(c *gin.Context) {
	v2, tabID, ok := h.resolveClientV2(c)
	if !ok {
		return
	}
	if !h.checkV2AvailableClient(c, v2) {
		return
	}

	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, http.StatusBadRequest, "invalid request body", "")
		return
	}

	username := h.getUsername(c)
	start := time.Now()
	paramsJSON := fmt.Sprintf(`{"dir": %v, "tab_id": %q}`, req.Dir, tabID)
	auditTarget := req.Key
	if tabID != "default" {
		auditTarget = fmt.Sprintf("[tab=%s] %s", tabID, req.Key)
	}

	err := v2.Delete(req.Key, req.Dir)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		h.markTabError(tabID, err)
		h.logAudit(username, "delete", auditTarget, paramsJSON, err.Error(), durationMs, false)
		h.writeErrorFromEtcd(c, err)
		return
	}

	h.logAudit(username, "delete", auditTarget, paramsJSON, "ok", durationMs, true)
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *KVHandler) handleV2Separator(c *gin.Context) {
	v2, _, ok := h.resolveClientV2(c)
	if !ok {
		return
	}
	if v2 == nil {
		h.writeError(c, http.StatusServiceUnavailable, "etcd v2 API is not available", "v2_unavailable")
		return
	}
	c.JSON(http.StatusOK, SeparatorResponse{Separator: v2.GetSeparator()})
}

func (h *KVHandler) handleV2Keys(c *gin.Context) {
	v2, tabID, ok := h.resolveClientV2(c)
	if !ok {
		return
	}
	if !h.checkV2AvailableClient(c, v2) {
		return
	}

	node, err := v2.Keys()
	if err != nil {
		h.markTabError(tabID, err)
		h.writeErrorFromEtcd(c, err)
		return
	}

	h.markTabOK(tabID)
	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

// ===== Helper Methods =====

// checkV2AvailableClient 仅在默认 Tab 路径上做"v2 是否预初始化成功"早退检查；
// per-Tab 路径下 v2 总是可用（available=true），实际 v2 是否启用在 Connect/Get 时探测。
func (h *KVHandler) checkV2AvailableClient(c *gin.Context, v2 *ClientV2) bool {
	if v2 == nil {
		h.writeError(c, http.StatusServiceUnavailable, "etcd v2 API is not available (requires --enable-v2=true)", "v2_unavailable")
		return false
	}
	if v2.override == nil && !v2.IsAvailable() {
		// 默认 Tab 且初始化失败
		h.writeError(c, http.StatusServiceUnavailable, "etcd v2 API is not available (requires --enable-v2=true)", "v2_unavailable")
		return false
	}
	return true
}

func (h *KVHandler) checkV2Available(c *gin.Context) bool {
	if h.v2 == nil || !h.v2.IsAvailable() {
		h.writeError(c, http.StatusServiceUnavailable, "etcd v2 API is not available (requires --enable-v2=true)", "v2_unavailable")
		return false
	}
	return true
}

func (h *KVHandler) writeError(c *gin.Context, status int, message string, code string) {
	c.JSON(status, ErrorResponse{
		Error:   message,
		Code:    code,
		Message: message,
	})
}

func (h *KVHandler) writeErrorFromEtcd(c *gin.Context, err error) {
	errStr := err.Error()

	// 检测权限不足
	if strings.Contains(errStr, "permission denied") || strings.Contains(errStr, "etcdserver: permission denied") {
		h.writeError(c, http.StatusForbidden, "permission denied: the configured etcd user does not have permission for this operation", "permission_denied")
		return
	}

	// 检测 key 不存在
	if strings.Contains(errStr, "key not found") || strings.Contains(errStr, "Key not found") {
		h.writeError(c, http.StatusNotFound, errStr, "key_not_found")
		return
	}

	// 其他错误
	h.writeError(c, http.StatusInternalServerError, errStr, "")
}
