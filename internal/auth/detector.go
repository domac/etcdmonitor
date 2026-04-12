package auth

import (
	"bytes"
	"errors"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/logger"
)

// DetectAuthRequired 探测 etcd 是否需要认证
// 返回 true 表示 Dashboard 需要登录，false 表示直接可用
func DetectAuthRequired(cfg *config.Config) bool {
	if cfg.Etcd.DiscoveryViaAPI != nil && *cfg.Etcd.DiscoveryViaAPI {
		return detectViaAPI(cfg)
	}
	return detectViaCtl(cfg)
}

// detectViaAPI 通过 HTTP API 检测认证状态
// 使用 POST /v3/auth/user/list（不带 token）来判断 etcd 是否启用认证：
//   - 200 → auth 开启且无需 token（不太常见，保守要求登录）
//   - 401 → auth 开启，需要凭据
//   - 501 或含 "not enabled" → auth 未启用
func detectViaAPI(cfg *config.Config) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	url := cfg.Etcd.Endpoint + "/v3/auth/user/list"

	req, err := http.NewRequest("POST", url, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		logger.Errorf("[Auth] Failed to create detection request: %v", err)
		logger.Warnf("[Auth] Unable to detect etcd auth status, conservatively requiring login")
		return true
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		logger.Warnf("[Auth] etcd endpoint unreachable (%v), conservatively requiring login", err)
		return true
	}
	defer resp.Body.Close()

	// 读取 body 用于判断
	bodyBytes := make([]byte, 1024)
	n, _ := resp.Body.Read(bodyBytes)
	bodyStr := strings.ToLower(string(bodyBytes[:n]))

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		logger.Infof("[Auth] etcd auth enabled (HTTP %d), Dashboard requires login", resp.StatusCode)
		return true
	case resp.StatusCode == http.StatusOK:
		// user list 成功，说明 auth 未启用（无需 token 即可操作）
		logger.Infof("[Auth] etcd auth not enabled (user list succeeded without token), Dashboard accessible without login")
		return false
	case strings.Contains(bodyStr, "not enabled") || strings.Contains(bodyStr, "not activated"):
		logger.Infof("[Auth] etcd auth not enabled, Dashboard accessible without login")
		return false
	default:
		// 其他响应码（如 501 Not Implemented），检查 body
		if strings.Contains(bodyStr, "not enabled") {
			logger.Infof("[Auth] etcd auth not enabled, Dashboard accessible without login")
			return false
		}
		logger.Warnf("[Auth] Unexpected response from etcd (HTTP %d), conservatively requiring login", resp.StatusCode)
		return true
	}
}

// detectViaCtl 通过 etcdctl 检测认证状态
// 使用 etcdctl user list（不带 --user）来判断 etcd 是否启用认证：
//   - exit code 0 → auth 未启用（任何人都能执行 user list），Dashboard 直接可用
//   - exit code != 0 + "user name is empty" → auth 已启用，Dashboard 需要登录
//   - exit code != 0 + 连接错误/命令不存在 → 保守策略，要求登录
func detectViaCtl(cfg *config.Config) bool {
	etcdctlPath := filepath.Join(cfg.Etcd.BinPath, "etcdctl")

	args := []string{
		"user", "list",
		"--endpoints=" + cfg.Etcd.Endpoint,
	}

	cmd := exec.Command(etcdctlPath, args...)
	cmd.Env = append(cmd.Environ(), "ETCDCTL_API=3")

	output, err := cmd.CombinedOutput()
	outputStr := strings.ToLower(string(output))

	if err == nil {
		// exit code 0: user list 成功，说明 auth 未启用（无需凭据即可操作）
		logger.Infof("[Auth] etcd auth not enabled (etcdctl user list succeeded without auth), Dashboard accessible without login")
		return false
	}

	// exit code != 0: 根据输出内容判断
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// etcdctl 执行了但返回非零退出码
		if strings.Contains(outputStr, "authentication is not enabled") {
			// auth 未启用，Dashboard 直接可用
			logger.Infof("[Auth] etcd auth not enabled, Dashboard accessible without login")
			return false
		} else if containsAuthError(outputStr) {
			// auth 已启用，需要凭据
			logger.Infof("[Auth] etcd auth enabled (detected via etcdctl user list), Dashboard requires login")
			return true
		} else if containsConnectionError(outputStr) {
			logger.Warnf("[Auth] etcd unreachable via etcdctl (%s), conservatively requiring login", strings.TrimSpace(string(output)))
			return true
		} else {
			logger.Warnf("[Auth] etcdctl user list returned error (%s), conservatively requiring login", strings.TrimSpace(string(output)))
			return true
		}
	} else {
		// 无法执行 etcdctl（文件不存在等）
		logger.Errorf("[Auth] etcdctl not found or cannot execute (%s: %v), conservatively requiring login", etcdctlPath, err)
		return true
	}
}

// containsAuthError 检查输出是否包含"auth 已启用但缺少凭据"的错误
// 注意："authentication is not enabled" 表示 auth 关闭，不属于此类
func containsAuthError(output string) bool {
	authKeywords := []string{
		"user name is empty",
		"invalid auth token",
		"auth: revision",
		"etcdserver: user name",
		"permission denied",
	}
	for _, kw := range authKeywords {
		if strings.Contains(output, kw) {
			return true
		}
	}
	return false
}

// containsConnectionError 检查输出是否包含连接相关错误
func containsConnectionError(output string) bool {
	connKeywords := []string{
		"connection refused",
		"connection reset",
		"no such host",
		"context deadline exceeded",
		"transport is closing",
		"unavailable",
	}
	for _, kw := range connKeywords {
		if strings.Contains(output, kw) {
			return true
		}
	}
	return false
}
