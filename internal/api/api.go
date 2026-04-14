package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"etcdmonitor/internal/auth"
	"etcdmonitor/internal/collector"
	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"
	"etcdmonitor/internal/logger"
	"etcdmonitor/internal/prefs"
	"etcdmonitor/internal/storage"

	"github.com/gin-gonic/gin"
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
	authHandler  *auth.AuthHandler
	version      string
}

// New 创建 API 实例
func New(cfg *config.Config, store *storage.Storage, c *collector.Collector, healthMgr *health.Manager, authRequired bool, sessionStore *auth.MemorySessionStore, prefsStore *prefs.FileStore, version string) *API {
	return &API{
		cfg:          cfg,
		store:        store,
		collector:    c,
		healthMgr:    healthMgr,
		authRequired: authRequired,
		sessionStore: sessionStore,
		prefsStore:   prefsStore,
		authHandler:  auth.NewAuthHandler(cfg, sessionStore, healthMgr, authRequired, version),
		version:      version,
	}
}

// SetupRoutes 注册路由到 Gin Engine
func (a *API) SetupRoutes(router *gin.Engine) *gin.RouterGroup {
	// 公开路由（不受认证中间件保护）
	router.POST("/api/auth/login", a.authHandler.HandleLogin)
	router.GET("/api/auth/status", a.authHandler.HandleAuthStatus)

	// 受保护路由组（认证模式下需要有效会话）
	protected := router.Group("/api")
	protected.Use(a.authMiddleware())
	{
		protected.POST("/auth/logout", a.authHandler.HandleLogout)
		protected.GET("/members", a.handleMembers)
		protected.GET("/current", a.handleCurrent)
		protected.GET("/range", a.handleRange)
		protected.GET("/status", a.handleStatus)
		protected.GET("/debug", a.handleDebug)
		protected.GET("/user/panel-config", a.handleGetPanelConfig)
		protected.PUT("/user/panel-config", a.handlePutPanelConfig)
	}

	return protected
}

// AuthRequired 返回是否需要认证（供外部使用）
func (a *API) AuthRequired() bool {
	return a.authRequired
}

// SecurityHeadersMiddleware 返回安全头 Gin 中间件（全局挂载）
func SecurityHeadersMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		if cfg.Server.TLSEnable {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

// GinZapLogger 返回自定义请求日志 Gin 中间件，使用项目的 Zap logger
func GinZapLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 跳过静态文件请求的日志记录
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/api/") {
			c.Next()
			return
		}

		start := time.Now()
		if c.Request.URL.RawQuery != "" {
			path = path + "?" + c.Request.URL.RawQuery
		}

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method

		if status >= 400 {
			logger.Warnf("[HTTP] %s %s %d %s client=%s", method, path, status, latency, c.ClientIP())
		} else {
			logger.Infof("[HTTP] %s %s %d %s", method, path, status, latency)
		}
	}
}

// GinRecovery 返回自定义 Recovery 中间件，panic 后写入 Zap
func GinRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				logger.Errorf("[HTTP] panic recovered: %v", err)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			}
		}()
		c.Next()
	}
}

// authMiddleware 返回认证 Gin 中间件
// authRequired=false 时直接放行；否则从 Cookie 或 Authorization header 获取 token
func (a *API) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !a.authRequired {
			c.Next()
			return
		}

		token := auth.ExtractToken(c.Request)
		if token == "" || !a.sessionStore.IsValid(token) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
			return
		}

		c.Next()
	}
}

// resolveMemberID 从请求中解析 member_id，不传则用默认
func (a *API) resolveMemberID(c *gin.Context) string {
	memberID := c.Query("member_id")
	if memberID == "" {
		memberID = a.collector.GetDefaultMemberID()
	}
	return memberID
}

// getUsername 从请求的 token 中获取用户名
func (a *API) getUsername(c *gin.Context) string {
	token := auth.ExtractToken(c.Request)
	if token == "" {
		return ""
	}
	session := a.sessionStore.Get(token)
	if session == nil {
		return ""
	}
	return session.Username
}

// handleGetPanelConfig 处理面板配置的读取
func (a *API) handleGetPanelConfig(c *gin.Context) {
	username := a.getUsername(c)
	if username == "" && a.authRequired {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	if username == "" {
		// 免认证模式返回默认配置
		c.JSON(http.StatusOK, prefs.DefaultConfig())
		return
	}

	cfg, err := a.prefsStore.Load(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load config failed"})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// handlePutPanelConfig 处理面板配置的保存
func (a *API) handlePutPanelConfig(c *gin.Context) {
	username := a.getUsername(c)
	if username == "" && a.authRequired {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no user identity"})
		return
	}

	var cfg prefs.PanelConfig
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 64*1024)).Decode(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	if err := a.prefsStore.Save(username, &cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "save config failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
