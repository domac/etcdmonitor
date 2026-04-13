package api

import (
	"encoding/json"
	"io"
	"net/http"

	"etcdmonitor/internal/auth"
	"etcdmonitor/internal/collector"
	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"
	"etcdmonitor/internal/prefs"
	"etcdmonitor/internal/storage"
)

// API 提供 REST 接口给前端 Dashboard
type API struct {
	cfg          *config.Config
	store        *storage.Storage
	collector    *collector.Collector
	healthMgr    *health.Manager
	authRequired bool
	sessionStore *auth.MemorySessionStore
	prefsStore   *prefs.FileStore
}

// New 创建 API 实例
func New(cfg *config.Config, store *storage.Storage, c *collector.Collector, healthMgr *health.Manager, authRequired bool, sessionStore *auth.MemorySessionStore, prefsStore *prefs.FileStore) *API {
	return &API{
		cfg:          cfg,
		store:        store,
		collector:    c,
		healthMgr:    healthMgr,
		authRequired: authRequired,
		sessionStore: sessionStore,
		prefsStore:   prefsStore,
	}
}

// SetupRoutes 注册路由
func (a *API) SetupRoutes(mux *http.ServeMux) {
	// 公开路由（不受认证中间件保护）
	mux.HandleFunc("/api/auth/login", a.securityHeaders(auth.HandleLogin(a.cfg, a.sessionStore, a.healthMgr)))
	mux.HandleFunc("/api/auth/status", a.securityHeaders(auth.HandleAuthStatus(a.sessionStore, a.authRequired)))

	// 受保护路由（认证模式下需要有效会话）
	mux.HandleFunc("/api/auth/logout", a.securityHeaders(a.authMiddleware(auth.HandleLogout(a.sessionStore))))
	mux.HandleFunc("/api/members", a.securityHeaders(a.authMiddleware(a.handleMembers)))
	mux.HandleFunc("/api/current", a.securityHeaders(a.authMiddleware(a.handleCurrent)))
	mux.HandleFunc("/api/range", a.securityHeaders(a.authMiddleware(a.handleRange)))
	mux.HandleFunc("/api/status", a.securityHeaders(a.authMiddleware(a.handleStatus)))
	mux.HandleFunc("/api/debug", a.securityHeaders(a.authMiddleware(a.handleDebug)))
	mux.HandleFunc("/api/user/panel-config", a.securityHeaders(a.authMiddleware(a.handlePanelConfig)))
}

// AuthMiddleware 返回认证中间件（供 KV 管理等外部模块使用）
func (a *API) AuthMiddleware() func(http.HandlerFunc) http.HandlerFunc {
	return a.authMiddleware
}

// SecurityHeaders 返回安全头中间件（供 KV 管理等外部模块使用）
func (a *API) SecurityHeaders() func(http.HandlerFunc) http.HandlerFunc {
	return a.securityHeaders
}

// authMiddleware 认证中间件
// authRequired=false 时直接放行；否则从 Cookie 或 Authorization header 获取 token
func (a *API) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authRequired {
			next(w, r)
			return
		}

		token := auth.ExtractToken(r)
		if token == "" || !a.sessionStore.IsValid(token) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "未认证"})
			return
		}

		next(w, r)
	}
}

// AuthRequired 返回是否需要认证（供外部使用）
func (a *API) AuthRequired() bool {
	return a.authRequired
}

// securityHeaders wraps a handler to add security response headers.
func (a *API) securityHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		if a.cfg.Server.TLSEnable {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next(w, r)
	}
}

// resolveMemberID 从请求中解析 member_id，不传则用默认
func (a *API) resolveMemberID(r *http.Request) string {
	memberID := r.URL.Query().Get("member_id")
	if memberID == "" {
		memberID = a.collector.GetDefaultMemberID()
	}
	return memberID
}

// getUsername 从请求的 token 中获取用户名
func (a *API) getUsername(r *http.Request) string {
	token := auth.ExtractToken(r)
	if token == "" {
		return ""
	}
	session := a.sessionStore.Get(token)
	if session == nil {
		return ""
	}
	return session.Username
}

// handlePanelConfig 处理面板配置的读取和保存
func (a *API) handlePanelConfig(w http.ResponseWriter, r *http.Request) {
	// 免认证模式下，前端应使用 localStorage，但后端仍返回默认配置
	username := a.getUsername(r)
	if username == "" && a.authRequired {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "未认证"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		if username == "" {
			// 免认证模式返回默认配置
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(prefs.DefaultConfig())
			return
		}
		cfg, err := a.prefsStore.Load(username)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "load config failed"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg)

	case http.MethodPut:
		if username == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "no user identity"})
			return
		}

		var cfg prefs.PanelConfig
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&cfg); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
			return
		}

		if err := a.prefsStore.Save(username, &cfg); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "save config failed"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "ok"})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}
