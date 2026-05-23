package tabs

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"etcdmonitor/internal/auth"
	"etcdmonitor/internal/config"
	"etcdmonitor/internal/storage"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TabHandler 提供 KV 多集群 Tab 的 HTTP 接口。
//
// 所有路径都强制按 `current_session.user_id` 隔离；跨用户访问统一返 404 KV_TAB_NOT_FOUND。
type TabHandler struct {
	mgr          *Manager
	store        *storage.Storage
	sessionStore *auth.MemorySessionStore
	logger       *zap.Logger
	cfg          *config.Config
}

// NewTabHandler 构造 TabHandler。
func NewTabHandler(
	mgr *Manager,
	store *storage.Storage,
	sessionStore *auth.MemorySessionStore,
	logger *zap.Logger,
	cfg *config.Config,
) *TabHandler {
	return &TabHandler{
		mgr:          mgr,
		store:        store,
		sessionStore: sessionStore,
		logger:       logger,
		cfg:          cfg,
	}
}

// RegisterRoutes 把 /api/kv/tabs/* 路由注册到给定路由组。
func (h *TabHandler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/kv/tabs")
	{
		g.GET("", h.handleList)
		g.POST("", h.handleCreate)
		g.PATCH("/:id", h.handlePatch)
		g.DELETE("/:id", h.handleDelete)
		g.POST("/:id/test", h.handleTest)
		g.PUT("/order", h.handleReorder)
	}
}

// ===== 请求 / 响应 DTO =====

// TabResponse 是返回给前端的 Tab 视图（不含密文与内部字段）。
type TabResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	Username      string `json:"username"`
	HasPassword   bool   `json:"has_password"`
	SortOrder     int    `json:"sort_order"`
	LastStatus    string `json:"last_status"`
	LastError     string `json:"last_error"`
	LastCheckedAt int64  `json:"last_checked_at"`
	IsDefault     bool   `json:"is_default"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

type createTabRequest struct {
	Endpoint string `json:"endpoint" binding:"required"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type patchTabRequest struct {
	Endpoint *string `json:"endpoint,omitempty"`
	Name     *string `json:"name,omitempty"`
	Username *string `json:"username,omitempty"`
	Password *string `json:"password,omitempty"`
}

type testTabRequest struct {
	Username *string `json:"username,omitempty"`
	Password *string `json:"password,omitempty"`
}

type reorderRequest struct {
	IDs []string `json:"ids" binding:"required"`
}

// errorBody 是统一错误响应。
type errorBody struct {
	Error            string `json:"error"`
	Code             string `json:"code,omitempty"`
	Message          string `json:"message,omitempty"`
	MatchedMemberURL string `json:"matched_member_url,omitempty"`
	Warning          string `json:"warning,omitempty"`
}

// ===== 助手 =====

// currentUserID 从会话取 user_id；未登录或会话无 user_id 返 (0, error)。
func (h *TabHandler) currentUserID(c *gin.Context) (int64, error) {
	token := auth.ExtractToken(c.Request)
	if token == "" {
		return 0, errors.New("no session token")
	}
	sess := h.sessionStore.Get(token)
	if sess == nil {
		return 0, errors.New("session not found or expired")
	}
	if sess.UserID <= 0 {
		// 兜底——理论上 fetchWithAuth 中间件之前会把无效 session 拦截
		return 0, errors.New("session has no user_id (login required)")
	}
	return sess.UserID, nil
}

// currentUsername 从会话取 username（用于审计）。失败返 "anonymous"。
func (h *TabHandler) currentUsername(c *gin.Context) string {
	token := auth.ExtractToken(c.Request)
	if token == "" {
		return "anonymous"
	}
	sess := h.sessionStore.Get(token)
	if sess == nil {
		return "anonymous"
	}
	return sess.Username
}

// schemeOf 从 endpoint 提取 scheme（用于审计 / 日志）。
func schemeOf(endpoint string) string {
	low := strings.ToLower(endpoint)
	switch {
	case strings.HasPrefix(low, "https://"):
		return "https"
	case strings.HasPrefix(low, "http://"):
		return "http"
	default:
		return "unknown"
	}
}

// hostOf 从 endpoint 提取 host 部分（用作默认显示名）。
//
// 例：https://etcd.prod.internal:2379/path → "etcd.prod.internal"
// 解析失败时返 endpoint 本身的简化形式。
func hostOf(endpoint string) string {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err == nil && u.Host != "" {
		// u.Hostname 自动剥离端口
		if h := u.Hostname(); h != "" {
			return h
		}
	}
	// fallback：去掉 scheme 前缀，再切到第一个 `:` 或 `/`
	s := endpoint
	for _, prefix := range []string{"https://", "http://", "HTTPS://", "HTTP://"} {
		s = strings.TrimPrefix(s, prefix)
	}
	if i := strings.IndexAny(s, ":/"); i >= 0 {
		s = s[:i]
	}
	return s
}

// logAudit 写一条审计；与 kvmanager.handler.go 的 logAudit 风格一致。
func (h *TabHandler) logAudit(username, op, target, params, result string,
	durationMs int64, success bool) {
	if h.store == nil {
		return
	}
	entry := storage.AuditEntry{
		Timestamp:  time.Now().Unix(),
		Username:   username,
		Operation:  op,
		Target:     target,
		Params:     params,
		Result:     result,
		DurationMs: durationMs,
		Success:    success,
	}
	if err := h.store.StoreAuditLog(entry); err != nil {
		h.logger.Warn("[KV-Tabs] write audit log failed",
			zap.String("operation", op), zap.Error(err))
	}
}

// LogCrossUserAttempt 记录跨用户访问尝试（被任何 handler 路径触发 ErrTabNotFound 时调用）。
//
// 导出此方法是为了让 internal/kvmanager.handler.go 中的 resolveClient* 也能写跨用户审计。
func (h *TabHandler) LogCrossUserAttempt(username, target, route string) {
	h.logAudit(username, "kv_tab_cross_user_attempt", target, fmt.Sprintf("route=%s", route),
		"forbidden", 0, false)
}

func tabToResponse(t *Tab) TabResponse {
	return TabResponse{
		ID:            t.ID,
		Name:          t.Name,
		Endpoint:      t.Endpoint,
		Username:      t.Username,
		HasPassword:   len(t.PasswordCipher) > 0,
		SortOrder:     t.SortOrder,
		LastStatus:    t.LastStatus,
		LastError:     t.LastError,
		LastCheckedAt: t.LastCheckedAt,
		IsDefault:     false,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
	}
}

// defaultTabResponse 构造默认 Tab 的视图——从 cfg 派生，永远在列表第一位。
func (h *TabHandler) defaultTabResponse() TabResponse {
	endpoint := h.cfg.EtcdFirstEndpoint()
	return TabResponse{
		ID:            "default",
		Name:          "default", // 固定名称，便于识别（host 已在 endpoint/title 中）
		Endpoint:      endpoint,
		Username:      h.cfg.Etcd.Username,
		HasPassword:   h.cfg.Etcd.Password != "",
		SortOrder:     -1, // 永远排在最前
		LastStatus:    "ok",
		LastError:     "",
		LastCheckedAt: 0,
		IsDefault:     true,
	}
}

// ===== 路由 handler =====

func (h *TabHandler) handleList(c *gin.Context) {
	userID, err := h.currentUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, errorBody{Error: err.Error(), Code: "UNAUTHORIZED"})
		return
	}

	tabs, err := h.mgr.repo.ListByUser(userID)
	if err != nil {
		h.logger.Error("[KV-Tabs] list failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errorBody{Error: "list tabs", Code: "INTERNAL"})
		return
	}

	out := make([]TabResponse, 0, len(tabs)+1)
	out = append(out, h.defaultTabResponse())
	for i := range tabs {
		out = append(out, tabToResponse(&tabs[i]))
	}
	c.JSON(http.StatusOK, gin.H{"tabs": out})
}

func (h *TabHandler) handleCreate(c *gin.Context) {
	start := time.Now()
	userID, err := h.currentUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, errorBody{Error: err.Error(), Code: "UNAUTHORIZED"})
		return
	}
	username := h.currentUsername(c)

	var req createTabRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorBody{Error: err.Error(), Code: "BAD_REQUEST"})
		return
	}

	endpoint := strings.TrimSpace(req.Endpoint)
	auditTarget := endpoint

	// 1. scheme 校验
	if err := h.mgr.validateScheme(endpoint); err != nil {
		h.logAudit(username, "kv_tab_create", auditTarget,
			fmt.Sprintf("scheme=%s username=%s password=***", schemeOf(endpoint), req.Username),
			"invalid_scheme", time.Since(start).Milliseconds(), false)
		c.JSON(http.StatusBadRequest, errorBody{
			Error: "endpoint must start with http:// or https://",
			Code:  "KV_TAB_INVALID_SCHEME",
		})
		return
	}

	// 2. per-user 限额
	count, err := h.mgr.repo.CountByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error(), Code: "INTERNAL"})
		return
	}
	if count >= 10 {
		h.logAudit(username, "kv_tab_create", auditTarget, "per-user limit reached",
			"limit_exceeded", time.Since(start).Milliseconds(), false)
		c.JSON(http.StatusConflict, errorBody{
			Error:   "per-user tab limit reached (10)",
			Code:    "KV_TAB_LIMIT_EXCEEDED",
			Message: "You have reached the per-user limit of 10 remote tabs.",
		})
		return
	}

	// 3. 连通性校验（仅 v3）
	connCfg := &ConnectionConfig{
		Endpoints: []string{endpoint},
		Username:  req.Username,
		Password:  req.Password,
		TLS:       tlsForEndpoint(endpoint),
	}
	status, errMsg := h.mgr.TestConnection(connCfg)
	if status != "ok" {
		var (
			code   = "KV_TAB_UNREACHABLE"
			httpSt = http.StatusBadRequest
		)
		if status == "auth_failed" {
			code = "KV_TAB_AUTH_FAILED"
			// 不能用 401——前端的 fetchWithAuth 把 401 当作"会话失效"
			// 直接跳 /login.html，会把用户踢出去。改用 400 配合 code 区分。
			httpSt = http.StatusBadRequest
		}
		h.logAudit(username, "kv_tab_create", auditTarget,
			fmt.Sprintf("scheme=%s username=%s password=***", schemeOf(endpoint), req.Username),
			fmt.Sprintf("%s: %s", status, errMsg), time.Since(start).Milliseconds(), false)
		c.JSON(httpSt, errorBody{
			Error: errMsg, Code: code, Message: errMsg,
		})
		return
	}

	// 4. 默认集群成员比对
	matched, matchedURL, degraded, err := h.mgr.IsDefaultClusterMember(endpoint)
	if err != nil {
		h.logger.Warn("[KV-Tabs] member-check error", zap.Error(err))
	}
	if matched {
		h.logAudit(username, "kv_tab_create", auditTarget,
			fmt.Sprintf("scheme=%s matched=%s", schemeOf(endpoint), matchedURL),
			"belongs_to_default", time.Since(start).Milliseconds(), false)
		c.JSON(http.StatusConflict, errorBody{
			Error:            "endpoint belongs to default cluster",
			Code:             "KV_TAB_BELONGS_TO_DEFAULT",
			MatchedMemberURL: matchedURL,
			Message: fmt.Sprintf("The endpoint %s is already a member of the default cluster (%s). "+
				"Use the default tab on the left — no need to add it again.", endpoint, matchedURL),
		})
		return
	}

	// 5. name 默认值
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = hostOf(endpoint)
	}

	// 6. 加密密码
	cipher, err := h.mgr.km.Encrypt([]byte(req.Password))
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error(), Code: "ENCRYPT_FAILED"})
		return
	}

	tab := &Tab{
		CreatedByUserID: userID,
		Name:            name,
		Endpoint:        endpoint,
		Username:        req.Username,
		PasswordCipher:  cipher,
		LastStatus:      "ok",
		LastCheckedAt:   time.Now().Unix(),
	}
	if err := h.mgr.repo.Create(tab); err != nil {
		h.logger.Error("[KV-Tabs] create failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error(), Code: "INTERNAL"})
		return
	}

	h.logAudit(username, "kv_tab_create", tab.ID,
		fmt.Sprintf("scheme=%s endpoint=%s username=%s password=***",
			schemeOf(endpoint), endpoint, req.Username),
		"ok", time.Since(start).Milliseconds(), true)

	resp := tabToResponse(tab)
	body := gin.H{"tab": resp}
	if degraded {
		body["warning"] = "degraded_member_check"
	}
	c.JSON(http.StatusCreated, body)
}

func (h *TabHandler) handlePatch(c *gin.Context) {
	start := time.Now()
	userID, err := h.currentUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, errorBody{Error: err.Error(), Code: "UNAUTHORIZED"})
		return
	}
	username := h.currentUsername(c)
	id := c.Param("id")

	// 默认 Tab 不可改
	if id == "default" {
		c.JSON(http.StatusBadRequest, errorBody{
			Error: "default tab cannot be modified", Code: "KV_TAB_DEFAULT_PROTECTED",
		})
		return
	}

	existing, err := h.mgr.repo.Get(id, userID)
	if err != nil {
		if errors.Is(err, ErrTabNotFound) {
			h.LogCrossUserAttempt(username, id, c.FullPath())
			c.JSON(http.StatusNotFound, errorBody{Error: "not found", Code: "KV_TAB_NOT_FOUND"})
			return
		}
		c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error(), Code: "INTERNAL"})
		return
	}

	var req patchTabRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorBody{Error: err.Error(), Code: "BAD_REQUEST"})
		return
	}

	patch := PatchFields{}

	// endpoint 变更必须重新校验全套
	if req.Endpoint != nil && *req.Endpoint != existing.Endpoint {
		newEndpoint := strings.TrimSpace(*req.Endpoint)
		if err := h.mgr.validateScheme(newEndpoint); err != nil {
			c.JSON(http.StatusBadRequest, errorBody{
				Error: err.Error(), Code: "KV_TAB_INVALID_SCHEME",
			})
			return
		}
		// 用新 endpoint + 拟用凭据校验连通性
		username2 := existing.Username
		if req.Username != nil {
			username2 = *req.Username
		}
		password2 := ""
		if req.Password != nil && *req.Password != "" {
			password2 = *req.Password
		} else {
			// 用旧密码探活新 endpoint
			plain, dErr := h.mgr.km.Decrypt(existing.PasswordCipher)
			if dErr != nil {
				c.JSON(http.StatusInternalServerError, errorBody{
					Error: "decrypt existing password failed", Code: "KV_TAB_KEK_MISSING",
				})
				return
			}
			password2 = string(plain)
		}
		connCfg := &ConnectionConfig{
			Endpoints: []string{newEndpoint},
			Username:  username2,
			Password:  password2,
			TLS:       tlsForEndpoint(newEndpoint),
		}
		status, errMsg := h.mgr.TestConnection(connCfg)
		if status != "ok" {
			code := "KV_TAB_UNREACHABLE"
			httpSt := http.StatusBadRequest
			if status == "auth_failed" {
				code = "KV_TAB_AUTH_FAILED"
				// 见 handleCreate 注释：401 会被前端 fetchWithAuth 当作会话失效跳登录
				httpSt = http.StatusBadRequest
			}
			c.JSON(httpSt, errorBody{Error: errMsg, Code: code})
			return
		}
		// 默认集群成员比对
		matched, matchedURL, _, _ := h.mgr.IsDefaultClusterMember(newEndpoint)
		if matched {
			c.JSON(http.StatusConflict, errorBody{
				Error:            "endpoint belongs to default cluster",
				Code:             "KV_TAB_BELONGS_TO_DEFAULT",
				MatchedMemberURL: matchedURL,
			})
			return
		}
		patch.Endpoint = &newEndpoint
		// endpoint 变更时把 status 重置（让前端 ⚠️ 立即清除）
		_ = h.mgr.repo.UpdateStatus(id, "ok", "", time.Now().Unix())
	}

	if req.Name != nil {
		patch.Name = req.Name
	}
	if req.Username != nil {
		patch.Username = req.Username
	}
	if req.Password != nil && *req.Password != "" {
		cipher, err := h.mgr.km.Encrypt([]byte(*req.Password))
		if err != nil {
			c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error(), Code: "ENCRYPT_FAILED"})
			return
		}
		patch.PasswordCipher = &cipher
	}

	if err := h.mgr.repo.UpdateByUser(id, userID, patch); err != nil {
		if errors.Is(err, ErrTabNotFound) {
			h.LogCrossUserAttempt(username, id, c.FullPath())
			c.JSON(http.StatusNotFound, errorBody{Error: "not found", Code: "KV_TAB_NOT_FOUND"})
			return
		}
		c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error(), Code: "INTERNAL"})
		return
	}

	h.logAudit(username, "kv_tab_update", id, fmt.Sprintf("password=***"),
		"ok", time.Since(start).Milliseconds(), true)

	updated, _ := h.mgr.repo.Get(id, userID)
	c.JSON(http.StatusOK, gin.H{"tab": tabToResponse(updated)})
}

func (h *TabHandler) handleDelete(c *gin.Context) {
	start := time.Now()
	userID, err := h.currentUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, errorBody{Error: err.Error(), Code: "UNAUTHORIZED"})
		return
	}
	username := h.currentUsername(c)
	id := c.Param("id")

	if id == "default" {
		c.JSON(http.StatusBadRequest, errorBody{
			Error: "default tab cannot be deleted", Code: "KV_TAB_DEFAULT_PROTECTED",
		})
		return
	}

	if err := h.mgr.repo.DeleteByUser(id, userID); err != nil {
		if errors.Is(err, ErrTabNotFound) {
			h.LogCrossUserAttempt(username, id, c.FullPath())
			c.JSON(http.StatusNotFound, errorBody{Error: "not found", Code: "KV_TAB_NOT_FOUND"})
			return
		}
		c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error(), Code: "INTERNAL"})
		return
	}

	h.logAudit(username, "kv_tab_delete", id, "", "ok",
		time.Since(start).Milliseconds(), true)
	c.Status(http.StatusNoContent)
}

func (h *TabHandler) handleTest(c *gin.Context) {
	userID, err := h.currentUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, errorBody{Error: err.Error(), Code: "UNAUTHORIZED"})
		return
	}
	username := h.currentUsername(c)
	id := c.Param("id")

	if id == "default" {
		c.JSON(http.StatusBadRequest, errorBody{
			Error: "default tab cannot be tested via this route",
			Code:  "KV_TAB_DEFAULT_PROTECTED",
		})
		return
	}

	tab, err := h.mgr.repo.Get(id, userID)
	if err != nil {
		if errors.Is(err, ErrTabNotFound) {
			h.LogCrossUserAttempt(username, id, c.FullPath())
			c.JSON(http.StatusNotFound, errorBody{Error: "not found", Code: "KV_TAB_NOT_FOUND"})
			return
		}
		c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error(), Code: "INTERNAL"})
		return
	}

	var req testTabRequest
	_ = c.ShouldBindJSON(&req) // body 为空也允许（用现有凭据测）

	connCfg := &ConnectionConfig{
		Endpoints: []string{tab.Endpoint},
		Username:  tab.Username,
		TLS:       tlsForEndpoint(tab.Endpoint),
	}
	if req.Username != nil {
		connCfg.Username = *req.Username
	}
	if req.Password != nil {
		connCfg.Password = *req.Password
	} else {
		plain, dErr := h.mgr.km.Decrypt(tab.PasswordCipher)
		if dErr != nil {
			c.JSON(http.StatusInternalServerError, errorBody{
				Error: "decrypt password", Code: "KV_TAB_KEK_MISSING",
			})
			return
		}
		connCfg.Password = string(plain)
	}

	status, errMsg := h.mgr.TestConnection(connCfg)
	resp := gin.H{"status": status}
	if status != "ok" {
		resp["error"] = errMsg
		httpSt := http.StatusBadRequest
		if status == "auth_failed" {
			// 见 handleCreate 注释：401 会被前端 fetchWithAuth 当作会话失效跳登录
			httpSt = http.StatusBadRequest
		}
		c.JSON(httpSt, resp)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *TabHandler) handleReorder(c *gin.Context) {
	start := time.Now()
	userID, err := h.currentUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, errorBody{Error: err.Error(), Code: "UNAUTHORIZED"})
		return
	}
	username := h.currentUsername(c)

	var req reorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorBody{Error: err.Error(), Code: "BAD_REQUEST"})
		return
	}

	if len(req.IDs) == 0 || req.IDs[0] != "default" {
		c.JSON(http.StatusBadRequest, errorBody{
			Error: "first ID must be 'default'", Code: "KV_TAB_DEFAULT_FIRST_REQUIRED",
		})
		return
	}

	// 剥离首位 default
	nonDefaultIDs := req.IDs[1:]

	if err := h.mgr.repo.UpdateOrderByUser(userID, nonDefaultIDs); err != nil {
		switch {
		case errors.Is(err, ErrOrderMismatch):
			c.JSON(http.StatusConflict, errorBody{
				Error: "ID count mismatch", Code: "KV_TAB_ORDER_MISMATCH",
			})
		case errors.Is(err, ErrTabNotFound):
			h.LogCrossUserAttempt(username, strings.Join(nonDefaultIDs, ","), c.FullPath())
			c.JSON(http.StatusBadRequest, errorBody{
				Error: "ID not found or cross-user", Code: "KV_TAB_NOT_FOUND",
			})
		default:
			c.JSON(http.StatusInternalServerError, errorBody{Error: err.Error(), Code: "INTERNAL"})
		}
		return
	}

	h.logAudit(username, "kv_tab_reorder", "",
		fmt.Sprintf("ids=%s", strings.Join(req.IDs, ",")),
		"ok", time.Since(start).Milliseconds(), true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
