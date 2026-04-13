package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"
	"etcdmonitor/internal/logger"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// HandleLogin 处理登录请求
// 通过 etcd 验证凭据，成功后创建会话并设置 Cookie
func HandleLogin(cfg *config.Config, store *MemorySessionStore, healthMgr *health.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}

		if req.Username == "" || req.Password == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password required"})
			return
		}

		// 通过 etcd SDK 验证凭据
		if err := verifyCredentials(cfg, healthMgr, req.Username, req.Password); err != nil {
			logger.Warnf("[Auth] Login failed for user %s: %v", req.Username, err)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "用户名或密码错误"})
			return
		}

		// 创建会话
		timeout := time.Duration(cfg.Server.SessionTimeout) * time.Second
		if timeout <= 0 {
			timeout = 1 * time.Hour
		}
		session, err := store.Create(req.Username, timeout)
		if err != nil {
			logger.Errorf("[Auth] Session creation failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
			return
		}

		// 设置 Cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "etcdmonitor_session",
			Value:    session.Token,
			Path:     "/",
			Expires:  session.ExpiresAt,
			HttpOnly: true,
			Secure:   cfg.Server.TLSEnable,
			SameSite: http.SameSiteLaxMode,
		})

		logger.Infof("[Auth] User %s logged in successfully", req.Username)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"username":      session.Username,
			"expires_at":    session.ExpiresAt.Unix(),
			"session_token": session.Token,
		})
	}
}

// HandleLogout 处理登出请求
func HandleLogout(store *MemorySessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		token := ExtractToken(r)
		if token != "" {
			session := store.Get(token)
			if session != nil {
				logger.Infof("[Auth] User %s logged out", session.Username)
			}
			store.Delete(token)
		}

		// 清除 Cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "etcdmonitor_session",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
		})

		writeJSON(w, http.StatusOK, map[string]string{"message": "已登出"})
	}
}

// HandleAuthStatus 返回当前认证状态
func HandleAuthStatus(store *MemorySessionStore, authRequired bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authRequired {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"auth_required": false,
			})
			return
		}

		token := ExtractToken(r)
		if token == "" {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"auth_required": true,
				"authenticated": false,
			})
			return
		}

		session := store.Get(token)
		if session == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"auth_required": true,
				"authenticated": false,
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"auth_required": true,
			"authenticated": true,
			"username":      session.Username,
			"expires_at":    session.ExpiresAt.Unix(),
		})
	}
}

// verifyCredentials 通过 etcd SDK 验证用户凭据
func verifyCredentials(cfg *config.Config, healthMgr *health.Manager, username, password string) error {
	etcdCfg := clientv3.Config{
		Endpoints:   healthMgr.HealthyEndpoints(),
		DialTimeout: 5 * time.Second,
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

// writeJSON 写入 JSON 响应
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
