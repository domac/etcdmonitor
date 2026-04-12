package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"etcdmonitor/internal/logger"
)

// Session 表示一个活跃的用户会话
type Session struct {
	Token     string
	Username  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// IsExpired 检查会话是否已过期
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

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
func (s *MemorySessionStore) Create(username string, timeout time.Duration) (*Session, error) {
	token, err := GenerateToken()
	if err != nil {
		return nil, err
	}

	session := &Session{
		Token:     token,
		Username:  username,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(timeout),
	}

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
