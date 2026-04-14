package auth

import (
	"context"
	"net/http"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"
	"etcdmonitor/internal/logger"
	"etcdmonitor/internal/tls"

	"github.com/gin-gonic/gin"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// LoginRequest 登录请求结构体
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// AuthHandler 认证相关的 HTTP 处理器
type AuthHandler struct {
	cfg          *config.Config
	sessionStore *MemorySessionStore
	healthMgr    *health.Manager
	authRequired bool
	version      string
}

// NewAuthHandler 创建 AuthHandler 实例
func NewAuthHandler(cfg *config.Config, sessionStore *MemorySessionStore, healthMgr *health.Manager, authRequired bool, version ...string) *AuthHandler {
	v := ""
	if len(version) > 0 {
		v = version[0]
	}
	return &AuthHandler{
		cfg:          cfg,
		sessionStore: sessionStore,
		healthMgr:    healthMgr,
		authRequired: authRequired,
		version:      v,
	}
}

// HandleLogin 处理登录请求
// 通过 etcd 验证凭据，成功后创建会话并设置 Cookie
func (h *AuthHandler) HandleLogin(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}

	// 通过 etcd SDK 验证凭据
	if err := verifyCredentials(h.cfg, h.healthMgr, req.Username, req.Password); err != nil {
		logger.Warnf("[Auth] Login failed for user %s: %v", req.Username, err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	// 创建会话
	timeout := time.Duration(h.cfg.Server.SessionTimeout) * time.Second
	if timeout <= 0 {
		timeout = 1 * time.Hour
	}
	session, err := h.sessionStore.Create(req.Username, timeout)
	if err != nil {
		logger.Errorf("[Auth] Session creation failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}

	// 设置 Cookie
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "etcdmonitor_session",
		Value:    session.Token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		Secure:   h.cfg.Server.TLSEnable,
		SameSite: http.SameSiteLaxMode,
	})

	logger.Infof("[Auth] User %s logged in successfully", req.Username)

	c.JSON(http.StatusOK, gin.H{
		"username":      session.Username,
		"expires_at":    session.ExpiresAt.Unix(),
		"session_token": session.Token,
	})
}

// HandleLogout 处理登出请求
func (h *AuthHandler) HandleLogout(c *gin.Context) {
	token := ExtractToken(c.Request)
	if token != "" {
		session := h.sessionStore.Get(token)
		if session != nil {
			logger.Infof("[Auth] User %s logged out", session.Username)
		}
		h.sessionStore.Delete(token)
	}

	// 清除 Cookie
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "etcdmonitor_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	c.JSON(http.StatusOK, gin.H{"message": "已登出"})
}

// HandleAuthStatus 返回当前认证状态
func (h *AuthHandler) HandleAuthStatus(c *gin.Context) {
	if !h.authRequired {
		c.JSON(http.StatusOK, gin.H{
			"auth_required": false,
			"app_version":   h.version,
		})
		return
	}

	token := ExtractToken(c.Request)
	if token == "" {
		c.JSON(http.StatusOK, gin.H{
			"auth_required": true,
			"authenticated": false,
			"app_version":   h.version,
		})
		return
	}

	session := h.sessionStore.Get(token)
	if session == nil {
		c.JSON(http.StatusOK, gin.H{
			"auth_required": true,
			"authenticated": false,
			"app_version":   h.version,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"auth_required": true,
		"authenticated": true,
		"username":      session.Username,
		"expires_at":    session.ExpiresAt.Unix(),
		"app_version":   h.version,
	})
}

// verifyCredentials 通过 etcd SDK 验证用户凭据
func verifyCredentials(cfg *config.Config, healthMgr *health.Manager, username, password string) error {
	etcdCfg := clientv3.Config{
		Endpoints:   healthMgr.HealthyEndpoints(),
		DialTimeout: 5 * time.Second,
	}

	// 应用 TLS 配置
	tlsCfg, err := tls.LoadClientTLSConfig(cfg)
	if err != nil {
		logger.Errorf("[Auth] Failed to load TLS configuration: %v", err)
	} else if tlsCfg != nil {
		etcdCfg.TLS = tlsCfg
	}

	client, err := clientv3.New(etcdCfg)
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = client.Authenticate(ctx, username, password)
	if err != nil {
		return errInvalidCredentials
	}

	logger.Infof("[Auth] SDK credential verification succeeded for user %s", username)
	return nil
}

var errInvalidCredentials = &InvalidCredentialsError{}

// InvalidCredentialsError 凭据无效错误
type InvalidCredentialsError struct{}

func (e *InvalidCredentialsError) Error() string {
	return "invalid credentials"
}
