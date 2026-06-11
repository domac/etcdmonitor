package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"etcdmonitor/internal/auth"
	"etcdmonitor/internal/config"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
}

// newTestAPI 创建一个测试用的 API 实例（无 collector/storage 依赖）
func newTestAPI(authRequired bool) (*API, *auth.MemorySessionStore) {
	cfg := &config.Config{}
	cfg.Server.SessionTimeout = 3600
	store := auth.NewMemorySessionStore()

	a := &API{
		cfg:          cfg,
		authRequired: authRequired,
		sessionStore: store,
		authHandler:  auth.NewAuthHandler(cfg, nil, store, nil, authRequired),
	}
	return a, store
}

// newTestRouter 创建一个带中间件的测试路由器
func newTestRouter(a *API) *gin.Engine {
	router := gin.New()
	return router
}

func TestAuthMiddleware_NoAuth(t *testing.T) {
	// 新语义：无论 authRequired 参数如何，middleware 均要求有效 session
	a, _ := newTestAPI(false)

	router := gin.New()
	router.GET("/api/test", a.authMiddleware(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (no bypass regardless of etcd auth), got %d", w.Code)
	}
}

func TestAuthMiddleware_RequiresAuth_NoCookie(t *testing.T) {
	a, _ := newTestAPI(true)

	router := gin.New()
	router.GET("/api/test", a.authMiddleware(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without cookie, got %d", w.Code)
	}
}

func TestAuthMiddleware_RequiresAuth_ValidCookie(t *testing.T) {
	a, store := newTestAPI(true)

	session, _ := store.Create(1, "testuser", 1*time.Hour)

	router := gin.New()
	router.GET("/api/test", a.authMiddleware(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: session.Token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with valid cookie, got %d", w.Code)
	}
}

func TestAuthMiddleware_RequiresAuth_InvalidCookie(t *testing.T) {
	a, _ := newTestAPI(true)

	router := gin.New()
	router.GET("/api/test", a.authMiddleware(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: "invalid-token"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with invalid cookie, got %d", w.Code)
	}
}

func TestHandleAuthStatus_NoAuth(t *testing.T) {
	// 新语义：auth_required 恒为 true（即便构造时传 etcdAuthEnabled=false）
	a, _ := newTestAPI(false)

	router := gin.New()
	router.GET("/api/auth/status", a.authHandler.HandleAuthStatus)

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["auth_required"] != true {
		t.Errorf("expected auth_required=true (new semantics), got %v", resp["auth_required"])
	}
	if resp["authenticated"] != false {
		t.Errorf("expected authenticated=false, got %v", resp["authenticated"])
	}
}

func TestHandleAuthStatus_AuthRequired_NotAuthenticated(t *testing.T) {
	a, _ := newTestAPI(true)

	router := gin.New()
	router.GET("/api/auth/status", a.authHandler.HandleAuthStatus)

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["auth_required"] != true {
		t.Errorf("expected auth_required=true, got %v", resp["auth_required"])
	}
	if resp["authenticated"] != false {
		t.Errorf("expected authenticated=false, got %v", resp["authenticated"])
	}
}

func TestHandleAuthStatus_AuthRequired_Authenticated(t *testing.T) {
	a, store := newTestAPI(true)

	session, _ := store.Create(1, "root", 1*time.Hour)

	router := gin.New()
	router.GET("/api/auth/status", a.authHandler.HandleAuthStatus)

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: session.Token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["auth_required"] != true {
		t.Errorf("expected auth_required=true, got %v", resp["auth_required"])
	}
	if resp["authenticated"] != true {
		t.Errorf("expected authenticated=true, got %v", resp["authenticated"])
	}
	if resp["username"] != "root" {
		t.Errorf("expected username=root, got %v", resp["username"])
	}
}

func TestHandleLogout(t *testing.T) {
	a, store := newTestAPI(true)

	session, _ := store.Create(1, "root", 1*time.Hour)

	router := gin.New()
	router.POST("/api/auth/logout", a.authHandler.HandleLogout)

	req := httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: session.Token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Session should be deleted
	if store.IsValid(session.Token) {
		t.Error("session should be invalid after logout")
	}

	// Cookie should be cleared
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "etcdmonitor_session" && c.MaxAge < 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected cookie to be cleared after logout")
	}
}

func TestHandleLogin_MissingFields(t *testing.T) {
	a, _ := newTestAPI(true)

	router := gin.New()
	router.POST("/api/auth/login", a.authHandler.HandleLogin)

	// Empty body
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing fields, got %d", w.Code)
	}
}

func TestSessionExpiry_MiddlewareRejects(t *testing.T) {
	a, store := newTestAPI(true)

	// Create session with very short timeout
	session, _ := store.Create(1, "testuser", 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	router := gin.New()
	router.GET("/api/test", a.authMiddleware(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: session.Token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for expired session, got %d", w.Code)
	}
}

// TestAuthStatus_NeverExpireSession_ReturnsZero 验证 ExpiresAt 零值的 session
// 在 /api/auth/status 响应中 expires_at 字段返回 0。
//
// 对应规范 local-user-auth → 「认证状态接口语义 / 已登录状态（永不过期 session）」
func TestAuthStatus_NeverExpireSession_ReturnsZero(t *testing.T) {
	a, store := newTestAPI(true)

	// 模拟 session_timeout=0 下登录创建的 session
	session, err := store.Create(1, "alice", 0)
	if err != nil {
		t.Fatalf("Create with timeout=0 failed: %v", err)
	}
	if !session.ExpiresAt.IsZero() {
		t.Fatalf("expected zero ExpiresAt for never-expire session, got %v", session.ExpiresAt)
	}

	router := gin.New()
	router.GET("/api/auth/status", a.authHandler.HandleAuthStatus)

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: session.Token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["authenticated"] != true {
		t.Errorf("expected authenticated=true, got %v", resp["authenticated"])
	}
	exp, ok := resp["expires_at"].(float64) // JSON 数字解码为 float64
	if !ok {
		t.Fatalf("expires_at not a number, got %T (%v)", resp["expires_at"], resp["expires_at"])
	}
	if exp != 0 {
		t.Errorf("expected expires_at=0 for never-expire session, got %v", exp)
	}
}

// TestAuthStatus_NormalSession_HasExpiresAt 验证普通过期 session 在 /api/auth/status
// 响应中 expires_at 仍然返回正常的 unix 时间戳，保证既有行为没被破坏。
func TestAuthStatus_NormalSession_HasExpiresAt(t *testing.T) {
	a, store := newTestAPI(true)

	session, _ := store.Create(1, "bob", 1*time.Hour)
	if session.ExpiresAt.IsZero() {
		t.Fatalf("normal session must not have zero ExpiresAt")
	}

	router := gin.New()
	router.GET("/api/auth/status", a.authHandler.HandleAuthStatus)

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: session.Token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	exp, ok := resp["expires_at"].(float64)
	if !ok {
		t.Fatalf("expires_at not a number, got %T", resp["expires_at"])
	}
	if int64(exp) <= time.Now().Unix() {
		t.Errorf("expected expires_at to be in the future, got %v", exp)
	}
	if int64(exp) != session.ExpiresAt.Unix() {
		t.Errorf("expires_at = %v, want %d", exp, session.ExpiresAt.Unix())
	}
}

// TestAuthMiddleware_NeverExpireSession_StillValid 验证永不过期 session
// 通过 authMiddleware：即便 ExpiresAt.IsZero()，IsValid 仍返回 true，受保护接口可访问。
func TestAuthMiddleware_NeverExpireSession_StillValid(t *testing.T) {
	a, store := newTestAPI(true)

	session, _ := store.Create(1, "alice", 0)

	router := gin.New()
	router.GET("/api/test", a.authMiddleware(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: session.Token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("never-expire session should pass middleware, got %d", w.Code)
	}
}
