package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/logger"
)

// HandleLogin 处理登录请求
// 通过 etcd 验证凭据，成功后创建会话并设置 Cookie
func HandleLogin(cfg *config.Config, store *MemorySessionStore) http.HandlerFunc {
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

		// 通过 etcd 验证凭据
		if err := verifyCredentials(cfg, req.Username, req.Password); err != nil {
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

// verifyCredentials 通过 etcd 验证用户凭据
func verifyCredentials(cfg *config.Config, username, password string) error {
	if cfg.Etcd.DiscoveryViaAPI != nil && *cfg.Etcd.DiscoveryViaAPI {
		return verifyViaAPI(cfg, username, password)
	}
	return verifyViaCtl(cfg, username, password)
}

// verifyViaAPI 通过 etcd HTTP API 验证凭据
func verifyViaAPI(cfg *config.Config, username, password string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	url := cfg.Etcd.Endpoint + "/v3/auth/authenticate"

	body, err := json.Marshal(map[string]string{
		"name":     username,
		"password": password,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // drain

	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return errInvalidCredentials
}

// verifyViaCtl 通过 etcdctl 验证凭据
func verifyViaCtl(cfg *config.Config, username, password string) error {
	// 安全检查：凭据中不允许包含 shell 元字符
	if containsDangerousChars(username) || containsDangerousChars(password) {
		return errInvalidCredentials
	}

	etcdctlPath := filepath.Join(cfg.Etcd.BinPath, "etcdctl")
	args := []string{
		"user", "list",
		"--endpoints=" + cfg.Etcd.Endpoint,
		"--user=" + username + ":" + password,
	}

	// 10 秒超时防止 etcdctl 挂起
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, etcdctlPath, args...)
	cmd.Env = append(cmd.Environ(), "ETCDCTL_API=3")

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Warnf("[Auth] etcdctl verify failed for user %s: %s (err: %v)", username, strings.TrimSpace(string(output)), err)
		return errInvalidCredentials
	}
	logger.Infof("[Auth] etcdctl verify succeeded for user %s", username)
	return nil
}

// containsDangerousChars 检查字符串中是否含有危险的 shell 元字符
func containsDangerousChars(s string) bool {
	dangerous := []string{"$", "`", ";", "|", "&", "<", ">", "(", ")", "\n", "\r"}
	for _, ch := range dangerous {
		if strings.Contains(s, ch) {
			return true
		}
	}
	return false
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
