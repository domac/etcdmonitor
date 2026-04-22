package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"etcdmonitor/internal/prefs"

	"github.com/gin-gonic/gin"
)

// newTestAPIWithPrefs 构造一个含 prefs.FileStore 的测试 API 实例。
func newTestAPIWithPrefs(t *testing.T) (*API, *prefs.FileStore, string) {
	t.Helper()
	a, _ := newTestAPI(true)
	dir := t.TempDir()
	store := prefs.NewFileStore(dir)
	a.prefsStore = store
	return a, store, dir
}

// newAuthedRouter 构造带认证中间件 + panel-config 路由的测试路由器。
// 返回 (router, sessionToken)。
func newAuthedRouter(t *testing.T, a *API, username string) (*gin.Engine, string) {
	t.Helper()
	session, _ := a.sessionStore.Create(username, 1*time.Hour)
	router := gin.New()
	protected := router.Group("/api")
	protected.Use(a.authMiddleware())
	protected.GET("/user/panel-config", a.handleGetPanelConfig)
	protected.PUT("/user/panel-config", a.handlePutPanelConfig)
	return router, session.Token
}

// TestPutPanelConfig_TooManyCards：body 含 8 张可见卡片，期望 400 + 不落盘。
func TestPutPanelConfig_TooManyCards(t *testing.T) {
	a, _, dir := newTestAPIWithPrefs(t)
	router, token := newAuthedRouter(t, a, "alice")

	cards := make([]prefs.CardPref, 0, 8)
	for i := 0; i < 8; i++ {
		cards = append(cards, prefs.CardPref{ID: "cardX" + string(rune('A'+i)), Visible: true, Order: i})
	}
	body, _ := json.Marshal(prefs.PanelConfig{Cards: cards})

	req := httptest.NewRequest("PUT", "/api/user/panel-config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
	var respBody map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("response body not JSON: %v (%s)", err, w.Body.String())
	}
	if respBody["error"] != "too many visible cards" {
		t.Errorf("error=%v, want 'too many visible cards'", respBody["error"])
	}
	if max, ok := respBody["max"].(float64); !ok || int(max) != prefs.MaxVisibleCards {
		t.Errorf("max=%v, want %d", respBody["max"], prefs.MaxVisibleCards)
	}

	// 文件不得被写入
	if _, err := os.Stat(filepath.Join(dir, "alice.json")); !os.IsNotExist(err) {
		t.Errorf("alice.json should NOT exist after rejected request; stat err = %v", err)
	}
}

// TestPutPanelConfig_AtLimit_OK：body 含恰好 7 张可见卡片，期望 200 + 落盘。
func TestPutPanelConfig_AtLimit_OK(t *testing.T) {
	a, _, dir := newTestAPIWithPrefs(t)
	router, token := newAuthedRouter(t, a, "bob")

	cards := make([]prefs.CardPref, 0, 7)
	for i := 0; i < 7; i++ {
		cards = append(cards, prefs.CardPref{ID: "cardX" + string(rune('A'+i)), Visible: true, Order: i})
	}
	body, _ := json.Marshal(prefs.PanelConfig{Cards: cards})

	req := httptest.NewRequest("PUT", "/api/user/panel-config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	// 文件应被写入
	if _, err := os.Stat(filepath.Join(dir, "bob.json")); err != nil {
		t.Errorf("bob.json should exist after successful save; err = %v", err)
	}
}

// TestGetPanelConfig_LegacyFile：老格式 JSON（无 cards 字段）应正确读取，
// 返回的 cards 为 nil/空（由前端 merge 补齐）。
func TestGetPanelConfig_LegacyFile(t *testing.T) {
	a, _, dir := newTestAPIWithPrefs(t)
	router, token := newAuthedRouter(t, a, "carol")

	// 预先写入老格式 fixture
	legacy := `{"panels":[{"id":"chartRaftProposals","visible":true,"order":0}]}`
	if err := os.WriteFile(filepath.Join(dir, "carol.json"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/user/panel-config", nil)
	req.AddCookie(&http.Cookie{Name: "etcdmonitor_session", Value: token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}

	var cfg prefs.PanelConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("response not PanelConfig JSON: %v", err)
	}
	if len(cfg.Cards) != 0 {
		t.Errorf("legacy file should return empty cards, got %d: %+v", len(cfg.Cards), cfg.Cards)
	}
	if len(cfg.Panels) == 0 {
		t.Error("panels should be populated by mergeWithDefaults")
	}
}
