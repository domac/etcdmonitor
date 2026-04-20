package auth

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"
	"etcdmonitor/internal/logger"
	"etcdmonitor/internal/storage"

	"github.com/gin-gonic/gin"
)

// LoginRequest 登录请求结构体
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// ChangePasswordRequest 修改密码请求结构体（零 token 设计：凭 username + old_password 授权）
type ChangePasswordRequest struct {
	Username    string `json:"username" binding:"required"`
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// AuthHandler 认证相关的 HTTP 处理器
type AuthHandler struct {
	cfg          *config.Config
	store        *storage.Storage
	sessionStore *MemorySessionStore
	healthMgr    *health.Manager
	authRequired bool // 仅用于元信息展示，不再参与门禁
	version      string
	dataDir      string // 初始密码文件所在目录
}

// NewAuthHandler 创建 AuthHandler 实例
func NewAuthHandler(cfg *config.Config, store *storage.Storage, sessionStore *MemorySessionStore, healthMgr *health.Manager, authRequired bool, version ...string) *AuthHandler {
	v := ""
	if len(version) > 0 {
		v = version[0]
	}
	return &AuthHandler{
		cfg:          cfg,
		store:        store,
		sessionStore: sessionStore,
		healthMgr:    healthMgr,
		authRequired: authRequired,
		version:      v,
		dataDir:      filepath.Dir(cfg.Storage.DBPath),
	}
}

// DataDir 返回 auth 使用的数据目录（供外部计算 initial-admin-password 文件路径）
func (h *AuthHandler) DataDir() string {
	return h.dataDir
}

// logAudit 写入审计日志（非阻塞）
func (h *AuthHandler) logAudit(username, operation, target, result string, durationMs int64, success bool) {
	if h.store == nil {
		return
	}
	entry := storage.AuditEntry{
		Timestamp:  time.Now().Unix(),
		Username:   username,
		Operation:  operation,
		Target:     target,
		Result:     result,
		DurationMs: durationMs,
		Success:    success,
	}
	if err := h.store.StoreAuditLog(entry); err != nil {
		logger.Warnf("[Auth] Failed to write audit log: %v", err)
	}
}

// formatLockedError 返回统一的锁定错误信息（精确到分钟，避免 timing 泄露）
func formatLockedError(lockedUntil int64) string {
	remaining := lockedUntil - time.Now().Unix()
	if remaining < 0 {
		remaining = 0
	}
	minutes := (remaining + 59) / 60 // 向上取整
	return "账户已锁定，请 " + itoa(int(minutes)) + " 分钟后再试"
}

func itoa(n int) string {
	// 避免 strconv 依赖（小工具），手动实现 int → string
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// handleFailedAttempt 密码错误时统一处理：累加 failed_attempts，达到阈值则锁定
func (h *AuthHandler) handleFailedAttempt(username string) {
	n, err := h.store.IncrementFailedAttempts(username)
	if err != nil {
		// 用户不存在或 DB 故障；静默处理，避免信息泄露
		return
	}
	threshold := h.cfg.Auth.LockoutThreshold
	if threshold <= 0 {
		threshold = 5
	}
	if n >= threshold {
		duration := h.cfg.Auth.LockoutDurationSeconds
		if duration <= 0 {
			duration = 900
		}
		until := time.Now().Unix() + int64(duration)
		if err := h.store.SetLocked(username, until); err != nil {
			logger.Warnf("[Auth] SetLocked failed for %s: %v", username, err)
		}
	}
}

// HandleLogin 处理登录请求 —— 校验本地 users 表
func (h *AuthHandler) HandleLogin(c *gin.Context) {
	start := time.Now()
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}
	clientIP := c.ClientIP()

	user, err := h.store.GetUserByUsername(req.Username)
	if err != nil {
		// 用户不存在：统一错误信息，不暴露
		elapsed := time.Since(start).Milliseconds()
		h.logAudit(req.Username, "login", clientIP, "用户名或密码错误", elapsed, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	// 锁定检查优先于 bcrypt 比对（防 timing 泄露）
	now := time.Now()
	if user.IsLocked(now) {
		elapsed := time.Since(start).Milliseconds()
		msg := formatLockedError(user.LockedUntil)
		h.logAudit(req.Username, "login", clientIP, msg, elapsed, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": msg})
		return
	}

	// bcrypt 比对
	if !ComparePassword(user.PasswordHash, req.Password) {
		h.handleFailedAttempt(req.Username)
		// 重新查询以获取最新锁定状态，用于审计
		if u2, err2 := h.store.GetUserByUsername(req.Username); err2 == nil && u2.IsLocked(time.Now()) {
			h.logAudit(req.Username, "login_lockout", clientIP, "账号已锁定", 0, true)
		}
		elapsed := time.Since(start).Milliseconds()
		h.logAudit(req.Username, "login", clientIP, "用户名或密码错误", elapsed, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	// 凭据正确 —— 根据 must_change_password 决定响应
	if user.MustChangePassword {
		// 不签发 session，返回标志让前端跳改密页
		elapsed := time.Since(start).Milliseconds()
		logger.Infof("[Auth] User %s authenticated but must change password", req.Username)
		h.logAudit(req.Username, "login", clientIP, "must_change_password", elapsed, true)
		c.JSON(http.StatusOK, gin.H{
			"username":               user.Username,
			"must_change_password":   true,
			"authenticated":          false,
		})
		return
	}

	// 正常登录：重置锁定计数、更新 last_login_at、签发 session
	_ = h.store.ResetLoginState(req.Username)
	_ = h.store.UpdateLastLogin(req.Username)

	timeout := time.Duration(h.cfg.Server.SessionTimeout) * time.Second
	if timeout <= 0 {
		timeout = 1 * time.Hour
	}
	session, err := h.sessionStore.Create(req.Username, timeout)
	if err != nil {
		elapsed := time.Since(start).Milliseconds()
		logger.Errorf("[Auth] Session creation failed: %v", err)
		h.logAudit(req.Username, "login", clientIP, "session creation failed", elapsed, false)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "etcdmonitor_session",
		Value:    session.Token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		Secure:   h.cfg.Server.TLSEnable,
		SameSite: http.SameSiteLaxMode,
	})

	elapsed := time.Since(start).Milliseconds()
	logger.Infof("[Auth] User %s logged in successfully", req.Username)
	h.logAudit(req.Username, "login", clientIP, "ok", elapsed, true)

	c.JSON(http.StatusOK, gin.H{
		"username":              session.Username,
		"expires_at":            session.ExpiresAt.Unix(),
		"session_token":         session.Token,
		"must_change_password":  false,
		"authenticated":         true,
	})
}

// HandleChangePassword 处理改密请求 —— 零 token 设计，凭 username + old_password 授权
func (h *AuthHandler) HandleChangePassword(c *gin.Context) {
	start := time.Now()
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username, old_password and new_password required"})
		return
	}
	clientIP := c.ClientIP()

	user, err := h.store.GetUserByUsername(req.Username)
	if err != nil {
		elapsed := time.Since(start).Milliseconds()
		h.logAudit(req.Username, "change_password", clientIP, "用户名或旧密码错误", elapsed, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或旧密码错误"})
		return
	}

	// 锁定检查
	if user.IsLocked(time.Now()) {
		elapsed := time.Since(start).Milliseconds()
		msg := formatLockedError(user.LockedUntil)
		h.logAudit(req.Username, "change_password", clientIP, msg, elapsed, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": msg})
		return
	}

	// 校验 old_password（与 login 共享 failed_attempts）
	if !ComparePassword(user.PasswordHash, req.OldPassword) {
		h.handleFailedAttempt(req.Username)
		if u2, err2 := h.store.GetUserByUsername(req.Username); err2 == nil && u2.IsLocked(time.Now()) {
			h.logAudit(req.Username, "login_lockout", clientIP, "账号已锁定", 0, true)
		}
		elapsed := time.Since(start).Milliseconds()
		h.logAudit(req.Username, "change_password", clientIP, "用户名或旧密码错误", elapsed, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或旧密码错误"})
		return
	}

	// new_password 策略校验（不计入 failed_attempts）
	minLen := h.cfg.Auth.MinPasswordLength
	if minLen <= 0 {
		minLen = 8
	}
	if len(req.NewPassword) < minLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码长度不足，至少 " + itoa(minLen) + " 位"})
		return
	}
	if req.NewPassword == req.OldPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码不能与旧密码相同"})
		return
	}

	newHash, err := HashPassword(req.NewPassword, h.cfg.Auth.BcryptCost)
	if err != nil {
		logger.Errorf("[Auth] HashPassword failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}

	// 重要：UpdatePassword 会清零 failed_attempts / locked_until；must_change 置 0
	if err := h.store.UpdatePassword(req.Username, newHash, false); err != nil {
		logger.Errorf("[Auth] UpdatePassword failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}

	// 首次改密（must_change=1 → 0）触发 initial-admin-password 文件自毁
	if user.MustChangePassword {
		if err := DeleteInitialPasswordFile(h.dataDir); err != nil {
			logger.Warnf("[Auth] Failed to delete initial password file: %v", err)
			h.logAudit("system", "initial_password_file_deleted", "system", err.Error(), 0, false)
		} else {
			h.logAudit("system", "initial_password_file_deleted", "system", "ok", 0, true)
		}
	}

	elapsed := time.Since(start).Milliseconds()
	logger.Infof("[Auth] User %s changed password successfully", req.Username)
	h.logAudit(req.Username, "change_password", clientIP, "ok", elapsed, true)

	c.JSON(http.StatusOK, gin.H{
		"message": "密码修改成功，请使用新密码登录",
	})
}

// HandleLogout 处理登出请求
func (h *AuthHandler) HandleLogout(c *gin.Context) {
	token := ExtractToken(c.Request)
	username := ""
	if token != "" {
		session := h.sessionStore.Get(token)
		if session != nil {
			username = session.Username
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

	if username != "" {
		h.logAudit(username, "logout", c.ClientIP(), "ok", 0, true)
	}

	c.JSON(http.StatusOK, gin.H{"message": "已登出"})
}

// HandleAuthStatus 返回当前认证状态
// auth_required 恒为 true；不再返回 must_change_password（仅由 login 响应携带）
func (h *AuthHandler) HandleAuthStatus(c *gin.Context) {
	initialSetupPending := InitialPasswordFileExists(h.dataDir)

	token := ExtractToken(c.Request)
	if token == "" {
		c.JSON(http.StatusOK, gin.H{
			"auth_required":           true,
			"authenticated":           false,
			"initial_setup_pending":   initialSetupPending,
			"app_version":             h.version,
			"ops_enabled":             h.cfg.OpsEnabled(),
			"etcd_auth_required":      h.authRequired,
		})
		return
	}

	session := h.sessionStore.Get(token)
	if session == nil {
		c.JSON(http.StatusOK, gin.H{
			"auth_required":           true,
			"authenticated":           false,
			"initial_setup_pending":   initialSetupPending,
			"app_version":             h.version,
			"ops_enabled":             h.cfg.OpsEnabled(),
			"etcd_auth_required":      h.authRequired,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"auth_required":           true,
		"authenticated":           true,
		"username":                session.Username,
		"expires_at":              session.ExpiresAt.Unix(),
		"initial_setup_pending":   initialSetupPending,
		"app_version":             h.version,
		"ops_enabled":             h.cfg.OpsEnabled(),
		"etcd_auth_required":      h.authRequired,
	})
}

// ErrInvalidCredentials 保留以兼容其它使用点
var ErrInvalidCredentials = errors.New("invalid credentials")

// 保留 prefixHasBearer 等小工具的位置；strings 占位避免未来有用
var _ = strings.Contains