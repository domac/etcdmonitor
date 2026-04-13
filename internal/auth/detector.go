package auth

import (
	"context"
	"strings"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/logger"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// DetectAuthRequired 探测 etcd 是否需要认证
// 返回 true 表示 Dashboard 需要登录，false 表示直接可用
func DetectAuthRequired(cfg *config.Config) bool {
	// 创建无凭据的临时客户端进行检测
	etcdCfg := clientv3.Config{
		Endpoints:   []string{cfg.Etcd.Endpoint},
		DialTimeout: 5 * time.Second,
	}

	client, err := clientv3.New(etcdCfg)
	if err != nil {
		logger.Warnf("[Auth] Failed to create etcd client (%v), conservatively requiring login", err)
		return true
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 优先尝试 AuthStatus()（etcd 3.5+）
	authResp, err := client.AuthStatus(ctx)
	if err == nil {
		if authResp.Enabled {
			logger.Infof("[Auth] etcd auth enabled (AuthStatus API), Dashboard requires login")
			return true
		}
		logger.Infof("[Auth] etcd auth not enabled (AuthStatus API), Dashboard accessible without login")
		return false
	}

	// AuthStatus 不可用（etcd 3.4 或更早），fallback 到 UserList 探测
	logger.Debugf("[Auth] AuthStatus not available (%v), falling back to UserList detection", err)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	_, err = client.UserList(ctx2)
	if err == nil {
		// UserList 成功且无需凭据 → auth 未启用
		logger.Infof("[Auth] etcd auth not enabled (UserList succeeded without credentials), Dashboard accessible without login")
		return false
	}

	errStr := strings.ToLower(err.Error())

	// "authentication is not enabled" → auth 未启用
	if strings.Contains(errStr, "not enabled") || strings.Contains(errStr, "not activated") {
		logger.Infof("[Auth] etcd auth not enabled, Dashboard accessible without login")
		return false
	}

	// 权限相关错误 → auth 已启用
	if strings.Contains(errStr, "user name is empty") ||
		strings.Contains(errStr, "permission denied") ||
		strings.Contains(errStr, "invalid auth token") ||
		strings.Contains(errStr, "etcdserver: user name") {
		logger.Infof("[Auth] etcd auth enabled (detected via UserList), Dashboard requires login")
		return true
	}

	// 连接错误或其他 → 保守策略
	logger.Warnf("[Auth] Unable to determine etcd auth status (%v), conservatively requiring login", err)
	return true
}
