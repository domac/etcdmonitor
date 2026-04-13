package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"etcdmonitor/internal/auth"
	"etcdmonitor/internal/config"
)

// newTestAPI 创建一个测试用的 API 实例（无 collector/storage 依赖）
func newTestAPI(authRequired bool) (*API, *auth.MemorySessionStore) {
	cfg := &config.Config{}
	cfg.Server.SessionTimeout = 3600
	store := auth.NewMemorySessionStore()

	a := &API{
		cfg:          cfg,
		authRequired: authRequired,
		sessionStore: store,
	}
	return a, store
}

func TestAuthMiddleware_NoAuth(t *testing.T) {
	a, _ := newTestAPI(false)

	handler := a.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when auth disabled, got %d", w.Code)
	}
}

func TestAuthMiddleware_RequiresAuth_NoCookie(t *testing.T) {
	a, _ := newTestAPI(true)

	handler := a.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without cookie, got %d", w.Code)
	}
}

func TestAuthMiddleware_RequiresAuth_ValidCookie(t *testing.T) {
	a, store := newTestAPI(true)

	session, _ := store.Create("testuser", 1*time.Hour)

	handler := a.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: session.Token})
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with valid cookie, got %d", w.Code)
	}
}

func TestAuthMiddleware_RequiresAuth_InvalidCookie(t *testing.T) {
	a, _ := newTestAPI(true)

	handler := a.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: "invalid-token"})
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with invalid cookie, got %d", w.Code)
	}
}

func TestHandleAuthStatus_NoAuth(t *testing.T) {
	_, store := newTestAPI(false)

	handler := auth.HandleAuthStatus(store, false)
	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["auth_required"] != false {
		t.Errorf("expected auth_required=false, got %v", resp["auth_required"])
	}
}

func TestHandleAuthStatus_AuthRequired_NotAuthenticated(t *testing.T) {
	_, store := newTestAPI(true)

	handler := auth.HandleAuthStatus(store, true)
	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	w := httptest.NewRecorder()
	handler(w, req)

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
	_, store := newTestAPI(true)

	session, _ := store.Create("root", 1*time.Hour)

	handler := auth.HandleAuthStatus(store, true)
	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: session.Token})
	w := httptest.NewRecorder()
	handler(w, req)

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
	_, store := newTestAPI(true)

	session, _ := store.Create("root", 1*time.Hour)

	handler := auth.HandleLogout(store)
	req := httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: session.Token})
	w := httptest.NewRecorder()
	handler(w, req)

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
	_, store := newTestAPI(true)
	cfg := &config.Config{}

	handler := auth.HandleLogin(cfg, store, nil)

	// Empty body
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing fields, got %d", w.Code)
	}
}

func TestHandleLogin_MethodNotAllowed(t *testing.T) {
	_, store := newTestAPI(true)
	cfg := &config.Config{}

	handler := auth.HandleLogin(cfg, store, nil)

	req := httptest.NewRequest("GET", "/api/auth/login", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", w.Code)
	}
}

func TestSessionExpiry_MiddlewareRejects(t *testing.T) {
	a, store := newTestAPI(true)

	// Create session with very short timeout
	session, _ := store.Create("testuser", 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	handler := a.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: session.Token})
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for expired session, got %d", w.Code)
	}
}
