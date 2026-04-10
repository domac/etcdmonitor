package api

import (
	"net/http"

	"etcdmonitor/internal/collector"
	"etcdmonitor/internal/config"
	"etcdmonitor/internal/storage"
)

// API 提供 REST 接口给前端 Dashboard
type API struct {
	cfg       *config.Config
	store     *storage.Storage
	collector *collector.Collector
}

// New 创建 API 实例
func New(cfg *config.Config, store *storage.Storage, c *collector.Collector) *API {
	return &API{
		cfg:       cfg,
		store:     store,
		collector: c,
	}
}

// SetupRoutes 注册路由
func (a *API) SetupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/members", a.securityHeaders(a.handleMembers))
	mux.HandleFunc("/api/current", a.securityHeaders(a.handleCurrent))
	mux.HandleFunc("/api/range", a.securityHeaders(a.handleRange))
	mux.HandleFunc("/api/status", a.securityHeaders(a.handleStatus))
	mux.HandleFunc("/api/debug", a.securityHeaders(a.handleDebug))
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
