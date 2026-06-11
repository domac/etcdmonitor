package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"etcdmonitor/internal/logger"
)

// Session 表示一个活跃的用户会话
type Session struct {
	Token     string
	UserID    int64 // users.id；登录时填充，用于按 user_id 隔离 KV Tab 等资源
	Username  string
	CreatedAt time.Time
	// ExpiresAt 为零值（IsZero() == true）时表示会话永不过期，
	// 对应配置 server.session_timeout=0 的语义。普通过期 session 使用具体时间戳。
	ExpiresAt time.Time
}

// IsExpired 检查会话是否已过期
//
// 当 ExpiresAt 为零值时，视为「永不过期」session（来自 server.session_timeout=0），
// 此处恒返回 false。
func (s *Session) IsExpired() bool {
	if s.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(s.ExpiresAt)
}

// ErrInvalidSessionTimeout 当传入 MemorySessionStore.Create 的 timeout 为负值时返回。
// 正常路径下 config.Load 已保证 SessionTimeout >= 0，调用方应自行做防御性兜底。
var ErrInvalidSessionTimeout = errors.New("session timeout must be >= 0 (0 means never expires)")

// GenerateToken 生成安全的会话令牌（32 字节 = 256 位，hex 编码后 64 字符）
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// MemorySessionStore 基于内存的会话存储
type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewMemorySessionStore 创建内存会话存储并启动后台清理
func NewMemorySessionStore() *MemorySessionStore {
	s := &MemorySessionStore{
		sessions: make(map[string]*Session),
		stopCh:   make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

// Create 创建新会话
//
// userID 必须 > 0（来自 users.id）；某些场景（旧测试 / 兼容）可传 0，
// 但相关功能（如 KV Tab 隔离）会拒绝该 session 访问。
//
// timeout 取值语义：
//   - timeout > 0  : 普通过期 session，ExpiresAt = now + timeout
//   - timeout == 0 : 永不过期 session，ExpiresAt 设为零值（time.Time{}）
//   - timeout < 0  : 非法，返回 ErrInvalidSessionTimeout（防御性，正常调用方不应触发）
func (s *MemorySessionStore) Create(userID int64, username string, timeout time.Duration) (*Session, error) {
	if timeout < 0 {
		return nil, ErrInvalidSessionTimeout
	}
	token, err := GenerateToken()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	session := &Session{
		Token:     token,
		UserID:    userID,
		Username:  username,
		CreatedAt: now,
	}
	if timeout > 0 {
		session.ExpiresAt = now.Add(timeout)
	}
	// timeout == 0 时 session.ExpiresAt 保持 time.Time{} 零值，IsExpired() 恒为 false

	s.mu.Lock()
	s.sessions[token] = session
	s.mu.Unlock()

	return session, nil
}

// Get 获取会话，如果不存在或已过期返回 nil
func (s *MemorySessionStore) Get(token string) *Session {
	s.mu.RLock()
	session, ok := s.sessions[token]
	s.mu.RUnlock()

	if !ok {
		return nil
	}
	if session.IsExpired() {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return nil
	}
	return session
}

// Delete 删除指定会话
func (s *MemorySessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// IsValid 检查 token 是否对应一个有效（未过期）的会话
func (s *MemorySessionStore) IsValid(token string) bool {
	return s.Get(token) != nil
}

// Stop 停止后台清理 goroutine
func (s *MemorySessionStore) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

// cleanupLoop 每小时清理过期会话
func (s *MemorySessionStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanup()
		case <-s.stopCh:
			return
		}
	}
}

// cleanup 删除所有已过期的会话
//
// 永不过期 session（ExpiresAt.IsZero()）通过 IsExpired() 返回 false 自动跳过；
// 仅显式 Logout / Delete(token) / 进程重启可移除。
func (s *MemorySessionStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for token, session := range s.sessions {
		if session.IsExpired() {
			delete(s.sessions, token)
			count++
		}
	}
	if count > 0 {
		logger.Infof("[Auth] Cleaned up %d expired sessions", count)
	}
}

// ExtractToken 从请求中提取 session token（优先 Authorization header，其次 Cookie）
func ExtractToken(r *http.Request) string {
	// 优先从 Authorization: Bearer <token> 获取
	if auth := r.Header.Get("Authorization"); auth != "" {
		const prefix = "Bearer "
		if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
			return auth[len(prefix):]
		}
	}
	// 回退到 Cookie
	if cookie, err := r.Cookie("etcdmonitor_session"); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return ""
}
