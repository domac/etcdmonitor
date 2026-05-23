package tabs

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"etcdmonitor/internal/auth"
	"etcdmonitor/internal/config"
	"etcdmonitor/internal/storage"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// newHandlerHarness 构造一个测试用 TabHandler + Manager + 内存 SQLite Storage。
//
// 不连真实 etcd——POST /api/kv/tabs 路径的"成功"分支需要真集群，
// 这里仅覆盖 list/patch/delete/order/cross-user 路径与各种错误码。
func newHandlerHarness(t *testing.T) (
	*TabHandler, *Manager, *storage.Storage, *auth.MemorySessionStore,
) {
	t.Helper()

	cfg := &config.Config{}
	cfg.Etcd.Endpoint = "http://127.0.0.1:65535" // 不可达，确保 TestConnection 快速失败
	cfg.Storage.DBPath = t.TempDir() + "/test.db"
	cfg.KVManager.ConnectTimeout = 1
	cfg.KVManager.RequestTimeout = 1
	cfg.KVManager.Separator = "/"

	store, err := storage.New(cfg)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	km, err := NewFileKeyManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileKeyManager: %v", err)
	}
	repo := NewSQLiteRepo(store.DB())
	mgr := NewManager(repo, km, cfg, nil)

	sess := auth.NewMemorySessionStore()
	logger := zap.NewNop()
	handler := NewTabHandler(mgr, store, sess, logger, cfg)

	t.Cleanup(func() { sess.Stop() })

	return handler, mgr, store, sess
}

// authedRequest 创建一个带 Authorization header 的测试请求。
func authedRequest(t *testing.T, sess *auth.MemorySessionStore, userID int64, username, method, path, body string) *http.Request {
	t.Helper()
	s, err := sess.Create(userID, username, time.Hour)
	if err != nil {
		t.Fatalf("sess.Create: %v", err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

// newRouter 注册 TabHandler 路由到一个测试 gin 引擎。
func newRouter(h *TabHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api")
	h.RegisterRoutes(g)
	return r
}

// seedTab 直接走 Manager.repo 入库，绕开 POST 校验链。
func seedTab(t *testing.T, mgr *Manager, userID int64, name, endpoint string) *Tab {
	t.Helper()
	cipher, _ := mgr.km.Encrypt([]byte("p"))
	tab := &Tab{
		CreatedByUserID: userID,
		Name:            name,
		Endpoint:        endpoint,
		Username:        "u",
		PasswordCipher:  cipher,
	}
	if err := mgr.repo.Create(tab); err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	return tab
}

// ===== 列表 =====

func TestHandler_List_OnlyDefault(t *testing.T) {
	h, _, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "u1", "GET", "/api/kv/tabs", ""))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	var body struct {
		Tabs []TabResponse `json:"tabs"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Tabs) != 1 {
		t.Errorf("got %d tabs; want 1 (default only)", len(body.Tabs))
	}
	if !body.Tabs[0].IsDefault {
		t.Errorf("first tab not default: %+v", body.Tabs[0])
	}
}

func TestHandler_List_PerUserIsolation(t *testing.T) {
	h, mgr, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	seedTab(t, mgr, 1, "alice-prod", "http://10.0.0.1:2379")
	seedTab(t, mgr, 1, "alice-dev", "http://10.0.0.2:2379")
	seedTab(t, mgr, 2, "bob-prod", "http://10.0.1.1:2379")

	// 用户 1 应该看到自己的 2 个 + 默认
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "alice", "GET", "/api/kv/tabs", ""))
	var body struct {
		Tabs []TabResponse `json:"tabs"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Tabs) != 3 {
		t.Errorf("user 1: got %d tabs; want 3", len(body.Tabs))
	}
	for _, tab := range body.Tabs {
		if strings.HasPrefix(tab.Name, "bob-") {
			t.Errorf("user 1 leaked bob's tab: %+v", tab)
		}
	}

	// 用户 2 应该看到自己的 1 个 + 默认
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 2, "bob", "GET", "/api/kv/tabs", ""))
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Tabs) != 2 {
		t.Errorf("user 2: got %d tabs; want 2", len(body.Tabs))
	}
}

func TestHandler_List_Unauthorized(t *testing.T) {
	h, _, _, _ := newHandlerHarness(t)
	r := newRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/kv/tabs", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d; want 401", w.Code)
	}
}

func TestHandler_List_NoCredentialFields(t *testing.T) {
	h, mgr, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	seedTab(t, mgr, 1, "secure", "https://etcd.prod:2379")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "u", "GET", "/api/kv/tabs", ""))

	body := w.Body.String()
	for _, banned := range []string{"password_cipher", "tls_enable", "tls_insecure_skip_verify",
		"created_by_user_id"} {
		if strings.Contains(body, banned) {
			t.Errorf("response contains forbidden field %q: %s", banned, body)
		}
	}
}

// ===== 创建（仅校验失败路径）=====

func TestHandler_Create_InvalidScheme(t *testing.T) {
	h, _, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	body := `{"endpoint":"unix:///tmp/etcd.sock"}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "u", "POST", "/api/kv/tabs", body))

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d; want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "KV_TAB_INVALID_SCHEME") {
		t.Errorf("missing KV_TAB_INVALID_SCHEME: %s", w.Body.String())
	}
}

func TestHandler_Create_PerUserLimit(t *testing.T) {
	h, mgr, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	// 用户 1 已有 10 个 Tab
	for i := 0; i < 10; i++ {
		seedTab(t, mgr, 1, "t", "http://x:2379")
	}

	body := `{"endpoint":"http://new:2379"}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "u", "POST", "/api/kv/tabs", body))

	if w.Code != http.StatusConflict {
		t.Errorf("got %d; want 409", w.Code)
	}
	if !strings.Contains(w.Body.String(), "KV_TAB_LIMIT_EXCEEDED") {
		t.Errorf("missing KV_TAB_LIMIT_EXCEEDED: %s", w.Body.String())
	}

	// 但用户 2 不受影响
	w = httptest.NewRecorder()
	body2 := `{"endpoint":"unix:///bad"}` // 用 invalid scheme 让请求通过 limit 检查、停在 scheme 校验
	r.ServeHTTP(w, authedRequest(t, sess, 2, "u2", "POST", "/api/kv/tabs", body2))
	if w.Code != http.StatusBadRequest {
		t.Errorf("user 2 got %d; want 400 (scheme failure, NOT limit)", w.Code)
	}
}

// ===== 删除 =====

func TestHandler_Delete_Default_Forbidden(t *testing.T) {
	h, _, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "u", "DELETE", "/api/kv/tabs/default", ""))

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d; want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "KV_TAB_DEFAULT_PROTECTED") {
		t.Errorf("missing KV_TAB_DEFAULT_PROTECTED: %s", w.Body.String())
	}
}

func TestHandler_Delete_CrossUser_404(t *testing.T) {
	h, mgr, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	tab := seedTab(t, mgr, 2, "bob-prod", "http://x:2379")

	// 用户 1 试图删用户 2 的 Tab → 404
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "alice", "DELETE", "/api/kv/tabs/"+tab.ID, ""))

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d; want 404 (cross-user)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "KV_TAB_NOT_FOUND") {
		t.Errorf("missing KV_TAB_NOT_FOUND: %s", w.Body.String())
	}

	// 用户 2 自己能删
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 2, "bob", "DELETE", "/api/kv/tabs/"+tab.ID, ""))
	if w.Code != http.StatusNoContent {
		t.Errorf("self delete: got %d; want 204", w.Code)
	}
}

// ===== PATCH（仅 name，不触发 endpoint 重校验）=====

func TestHandler_Patch_NameOnly(t *testing.T) {
	h, mgr, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	tab := seedTab(t, mgr, 1, "old", "http://x:2379")

	body := `{"name":"new-name"}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "u", "PATCH", "/api/kv/tabs/"+tab.ID, body))

	if w.Code != http.StatusOK {
		t.Errorf("got %d; want 200; body=%s", w.Code, w.Body.String())
	}

	got, _ := mgr.repo.Get(tab.ID, 1)
	if got.Name != "new-name" {
		t.Errorf("name = %q; want new-name", got.Name)
	}
	if string(got.PasswordCipher) != string(tab.PasswordCipher) {
		t.Error("password_cipher unexpectedly changed")
	}
}

func TestHandler_Patch_CrossUser_404(t *testing.T) {
	h, mgr, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	tab := seedTab(t, mgr, 2, "bob", "http://x:2379")

	body := `{"name":"hacked"}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "alice", "PATCH", "/api/kv/tabs/"+tab.ID, body))

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d; want 404", w.Code)
	}
}

// ===== 排序 =====

func TestHandler_Reorder_HappyPath(t *testing.T) {
	h, mgr, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	a := seedTab(t, mgr, 1, "A", "http://a:2379")
	b := seedTab(t, mgr, 1, "B", "http://b:2379")

	body, _ := json.Marshal(map[string]interface{}{"ids": []string{"default", b.ID, a.ID}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "u", "PUT", "/api/kv/tabs/order", string(body)))

	if w.Code != http.StatusOK {
		t.Errorf("got %d; want 200; body=%s", w.Code, w.Body.String())
	}

	list, _ := mgr.repo.ListByUser(1)
	if list[0].Name != "B" || list[1].Name != "A" {
		t.Errorf("order wrong: %s, %s", list[0].Name, list[1].Name)
	}
}

func TestHandler_Reorder_DefaultMustBeFirst(t *testing.T) {
	h, mgr, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	a := seedTab(t, mgr, 1, "A", "http://a:2379")

	body, _ := json.Marshal(map[string]interface{}{"ids": []string{a.ID, "default"}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "u", "PUT", "/api/kv/tabs/order", string(body)))

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d; want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "KV_TAB_DEFAULT_FIRST_REQUIRED") {
		t.Errorf("missing KV_TAB_DEFAULT_FIRST_REQUIRED: %s", w.Body.String())
	}
}

func TestHandler_Reorder_CountMismatch(t *testing.T) {
	h, mgr, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	a := seedTab(t, mgr, 1, "A", "http://a:2379")
	_ = seedTab(t, mgr, 1, "B", "http://b:2379")

	// 只提交 default + A，缺 B
	body, _ := json.Marshal(map[string]interface{}{"ids": []string{"default", a.ID}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "u", "PUT", "/api/kv/tabs/order", string(body)))

	if w.Code != http.StatusConflict {
		t.Errorf("got %d; want 409", w.Code)
	}
	if !strings.Contains(w.Body.String(), "KV_TAB_ORDER_MISMATCH") {
		t.Errorf("missing KV_TAB_ORDER_MISMATCH: %s", w.Body.String())
	}
}

func TestHandler_Reorder_OtherUserIDInArray(t *testing.T) {
	h, mgr, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	_ = seedTab(t, mgr, 1, "A", "http://a:2379")
	bobTab := seedTab(t, mgr, 2, "B", "http://b:2379")

	// 用户 1 的请求里塞用户 2 的 ID（数量正好对得上 1 个）
	body, _ := json.Marshal(map[string]interface{}{"ids": []string{"default", bobTab.ID}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "alice", "PUT", "/api/kv/tabs/order", string(body)))

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d; want 400 (cross-user)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "KV_TAB_NOT_FOUND") {
		t.Errorf("missing KV_TAB_NOT_FOUND: %s", w.Body.String())
	}
}

// ===== 多用户场景 E2E =====

func TestHandler_MultiUser_FullScenario(t *testing.T) {
	h, mgr, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	// 用户 A 创建 3 个 Tab
	for i := 0; i < 3; i++ {
		seedTab(t, mgr, 1, "A"+string(rune('1'+i)), "http://a:2379")
	}
	// 用户 B 创建 3 个 Tab
	bobTabs := make([]*Tab, 3)
	for i := 0; i < 3; i++ {
		bobTabs[i] = seedTab(t, mgr, 2, "B"+string(rune('1'+i)), "http://b:2379")
	}

	// (1) A 列表只看到自己的 3 + 默认
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "alice", "GET", "/api/kv/tabs", ""))
	var body struct {
		Tabs []TabResponse `json:"tabs"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Tabs) != 4 {
		t.Errorf("alice sees %d tabs; want 4", len(body.Tabs))
	}

	// (2) A 用 B 的 tab_id 调 GET 路径——TabHandler 没有按 tab_id 的 GET，
	//     但 PATCH/DELETE/test 会触发跨用户 404
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "alice", "DELETE",
		"/api/kv/tabs/"+bobTabs[0].ID, ""))
	if w.Code != http.StatusNotFound {
		t.Errorf("alice deletes bob's tab: got %d; want 404", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "alice", "PATCH",
		"/api/kv/tabs/"+bobTabs[1].ID, `{"name":"hax"}`))
	if w.Code != http.StatusNotFound {
		t.Errorf("alice patches bob's tab: got %d; want 404", w.Code)
	}

	// (3) A 排序时塞 B 的 ID → 400 NOT_FOUND
	body2, _ := json.Marshal(map[string]interface{}{
		"ids": []string{"default", bobTabs[0].ID, bobTabs[1].ID, bobTabs[2].ID},
	})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "alice", "PUT",
		"/api/kv/tabs/order", string(body2)))
	if w.Code != http.StatusBadRequest {
		t.Errorf("alice reorders with bob's IDs: got %d; want 400", w.Code)
	}

	// (4) B 的 Tab 完好无损
	bobList, _ := mgr.repo.ListByUser(2)
	if len(bobList) != 3 {
		t.Errorf("bob's tabs were affected: got %d; want 3", len(bobList))
	}
}

// ===== Test endpoint =====

func TestHandler_Test_RequiresAuth(t *testing.T) {
	h, _, _, _ := newHandlerHarness(t)
	r := newRouter(h)

	req := httptest.NewRequest("POST", "/api/kv/tabs/some-id/test",
		bytes.NewBufferString(`{}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d; want 401", w.Code)
	}
}

func TestHandler_Test_CrossUser404(t *testing.T) {
	h, mgr, _, sess := newHandlerHarness(t)
	r := newRouter(h)

	tab := seedTab(t, mgr, 2, "bob", "http://x:2379")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedRequest(t, sess, 1, "alice", "POST",
		"/api/kv/tabs/"+tab.ID+"/test", `{}`))

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d; want 404", w.Code)
	}
}
