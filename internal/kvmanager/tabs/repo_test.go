package tabs

import (
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

// newTestDB 用内存 SQLite 创建一个带 kv_cluster_tabs 表的测试数据库。
//
// 注意：表 schema 必须与 internal/storage/storage.go 的 initTables 一致。
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
		CREATE TABLE kv_cluster_tabs (
			id TEXT PRIMARY KEY,
			created_by_user_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			endpoint TEXT NOT NULL,
			username TEXT NOT NULL DEFAULT '',
			password_cipher BLOB NOT NULL DEFAULT x'',
			sort_order INTEGER NOT NULL,
			last_status TEXT NOT NULL DEFAULT 'unknown',
			last_error TEXT NOT NULL DEFAULT '',
			last_checked_at INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);
		CREATE INDEX idx_kv_cluster_tabs_user_sort
			ON kv_cluster_tabs(created_by_user_id, sort_order);
		CREATE INDEX idx_kv_cluster_tabs_user
			ON kv_cluster_tabs(created_by_user_id);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func newTab(userID int64, name string) *Tab {
	return &Tab{
		CreatedByUserID: userID,
		Name:            name,
		Endpoint:        "http://example.com:2379",
		Username:        "user",
		PasswordCipher:  []byte{1, 2, 3, 4},
	}
}

// TestSQLiteRepo_CreateAndGet 创建后能用归属用户读到，但他人读不到。
func TestSQLiteRepo_CreateAndGet(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))

	tab := newTab(42, "tab-A")
	if err := repo.Create(tab); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tab.ID == "" {
		t.Error("ID not assigned by Create")
	}
	if tab.SortOrder != 0 {
		t.Errorf("first tab SortOrder = %d; want 0", tab.SortOrder)
	}

	// 用归属用户读
	got, err := repo.Get(tab.ID, 42)
	if err != nil {
		t.Fatalf("Get(self): %v", err)
	}
	if got.Name != "tab-A" {
		t.Errorf("got name %q; want tab-A", got.Name)
	}

	// 用他人读 → ErrTabNotFound
	if _, err := repo.Get(tab.ID, 99); !errors.Is(err, ErrTabNotFound) {
		t.Errorf("Get(otherUser) = %v; want ErrTabNotFound", err)
	}
}

// TestSQLiteRepo_CreateRequiresUserID 必须强制 userID > 0。
func TestSQLiteRepo_CreateRequiresUserID(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))

	cases := []int64{0, -1}
	for _, uid := range cases {
		tab := &Tab{CreatedByUserID: uid, Name: "x", Endpoint: "http://x:2379"}
		if err := repo.Create(tab); !errors.Is(err, ErrUserIDRequired) {
			t.Errorf("Create(userID=%d) = %v; want ErrUserIDRequired", uid, err)
		}
	}
}

// TestSQLiteRepo_SortOrderAutoIncrement 多次 Create 必须 0,1,2,... 自增。
func TestSQLiteRepo_SortOrderAutoIncrement(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))

	for i := 0; i < 3; i++ {
		tab := newTab(1, "tab")
		if err := repo.Create(tab); err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
		if tab.SortOrder != i {
			t.Errorf("SortOrder #%d = %d; want %d", i, tab.SortOrder, i)
		}
	}
}

// TestSQLiteRepo_PerUserSortOrder 用户 A 与 B 的 sort_order 互不影响。
func TestSQLiteRepo_PerUserSortOrder(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))

	a1 := newTab(1, "A1")
	_ = repo.Create(a1) // sort=0
	b1 := newTab(2, "B1")
	_ = repo.Create(b1) // sort=0（per-user）
	a2 := newTab(1, "A2")
	_ = repo.Create(a2) // sort=1

	if a1.SortOrder != 0 || b1.SortOrder != 0 || a2.SortOrder != 1 {
		t.Errorf("sort_order per-user wrong: A1=%d B1=%d A2=%d", a1.SortOrder, b1.SortOrder, a2.SortOrder)
	}
}

// TestSQLiteRepo_ListByUser_FiltersAndOrders 列表必须仅返回归属用户、按 sort_order 升序。
func TestSQLiteRepo_ListByUser_FiltersAndOrders(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))

	_ = repo.Create(newTab(1, "A1"))
	_ = repo.Create(newTab(1, "A2"))
	_ = repo.Create(newTab(2, "B1"))

	listA, err := repo.ListByUser(1)
	if err != nil {
		t.Fatalf("ListByUser(1): %v", err)
	}
	if len(listA) != 2 {
		t.Fatalf("user 1 list size = %d; want 2", len(listA))
	}
	if listA[0].Name != "A1" || listA[1].Name != "A2" {
		t.Errorf("order wrong: %v", []string{listA[0].Name, listA[1].Name})
	}

	listB, _ := repo.ListByUser(2)
	if len(listB) != 1 || listB[0].Name != "B1" {
		t.Errorf("user 2 list = %+v", listB)
	}
}

// TestSQLiteRepo_UpdateByUser_PartialFields 仅更新非 nil 字段，跨用户返 ErrTabNotFound。
func TestSQLiteRepo_UpdateByUser_PartialFields(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))
	tab := newTab(1, "before")
	_ = repo.Create(tab)

	newName := "after"
	if err := repo.UpdateByUser(tab.ID, 1, PatchFields{Name: &newName}); err != nil {
		t.Fatalf("UpdateByUser: %v", err)
	}
	got, _ := repo.Get(tab.ID, 1)
	if got.Name != "after" {
		t.Errorf("name = %q; want after", got.Name)
	}
	if string(got.PasswordCipher) != string(tab.PasswordCipher) {
		t.Errorf("password_cipher unexpectedly changed")
	}

	// 跨用户更新 → ErrTabNotFound
	other := "X"
	if err := repo.UpdateByUser(tab.ID, 99, PatchFields{Name: &other}); !errors.Is(err, ErrTabNotFound) {
		t.Errorf("cross-user UpdateByUser = %v; want ErrTabNotFound", err)
	}
}

// TestSQLiteRepo_UpdateOrderByUser_HappyPath 全量重写顺序，仅更新当前用户的行。
func TestSQLiteRepo_UpdateOrderByUser_HappyPath(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))
	a := newTab(1, "A")
	b := newTab(1, "B")
	c := newTab(1, "C")
	_ = repo.Create(a)
	_ = repo.Create(b)
	_ = repo.Create(c)

	// 反转顺序
	if err := repo.UpdateOrderByUser(1, []string{c.ID, b.ID, a.ID}); err != nil {
		t.Fatalf("UpdateOrderByUser: %v", err)
	}
	list, _ := repo.ListByUser(1)
	if list[0].Name != "C" || list[1].Name != "B" || list[2].Name != "A" {
		t.Errorf("reorder wrong: %v %v %v", list[0].Name, list[1].Name, list[2].Name)
	}
}

// TestSQLiteRepo_UpdateOrderByUser_CountMismatch 数量不一致返 ErrOrderMismatch。
func TestSQLiteRepo_UpdateOrderByUser_CountMismatch(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))
	a := newTab(1, "A")
	b := newTab(1, "B")
	_ = repo.Create(a)
	_ = repo.Create(b)

	if err := repo.UpdateOrderByUser(1, []string{a.ID}); !errors.Is(err, ErrOrderMismatch) {
		t.Errorf("got %v; want ErrOrderMismatch", err)
	}
}

// TestSQLiteRepo_UpdateOrderByUser_OtherUserIDInArray 数组含他人 ID 返 ErrTabNotFound（不暴露存在性）。
func TestSQLiteRepo_UpdateOrderByUser_OtherUserIDInArray(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))
	a := newTab(1, "A")
	b := newTab(2, "B")
	_ = repo.Create(a)
	_ = repo.Create(b)

	// 用户 1 的请求里塞用户 2 的 ID（数量等于 1，刚好通过 count 校验，但归属不对）
	err := repo.UpdateOrderByUser(1, []string{b.ID})
	if !errors.Is(err, ErrTabNotFound) {
		t.Errorf("got %v; want ErrTabNotFound", err)
	}
}

// TestSQLiteRepo_DeleteByUser_FiltersOwner 删除必须归属用户，跨用户返 ErrTabNotFound。
func TestSQLiteRepo_DeleteByUser_FiltersOwner(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))
	tab := newTab(1, "A")
	_ = repo.Create(tab)

	if err := repo.DeleteByUser(tab.ID, 99); !errors.Is(err, ErrTabNotFound) {
		t.Errorf("cross-user delete = %v; want ErrTabNotFound", err)
	}
	// 用归属用户能删
	if err := repo.DeleteByUser(tab.ID, 1); err != nil {
		t.Errorf("self delete: %v", err)
	}
	// 已删后再删返 ErrTabNotFound
	if err := repo.DeleteByUser(tab.ID, 1); !errors.Is(err, ErrTabNotFound) {
		t.Errorf("delete-after-delete = %v; want ErrTabNotFound", err)
	}
}

// TestSQLiteRepo_CountByUser 仅计当前用户行。
func TestSQLiteRepo_CountByUser(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))
	_ = repo.Create(newTab(1, "A1"))
	_ = repo.Create(newTab(1, "A2"))
	_ = repo.Create(newTab(2, "B1"))

	a, _ := repo.CountByUser(1)
	b, _ := repo.CountByUser(2)
	c, _ := repo.CountByUser(3)
	if a != 2 || b != 1 || c != 0 {
		t.Errorf("count wrong: a=%d b=%d c=%d", a, b, c)
	}
}

// TestSQLiteRepo_UpdateStatus_NoUserFilter UpdateStatus 不带 userID（探活专用）。
func TestSQLiteRepo_UpdateStatus_NoUserFilter(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))
	tab := newTab(1, "A")
	_ = repo.Create(tab)

	if err := repo.UpdateStatus(tab.ID, "error", "boom", 1234567890); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ := repo.Get(tab.ID, 1)
	if got.LastStatus != "error" || got.LastError != "boom" || got.LastCheckedAt != 1234567890 {
		t.Errorf("status not written: %+v", got)
	}

	// 探活时 Tab 已被删除——UpdateStatus 不能报错
	_ = repo.DeleteByUser(tab.ID, 1)
	if err := repo.UpdateStatus(tab.ID, "ok", "", 0); err != nil {
		t.Errorf("UpdateStatus after delete returned error: %v", err)
	}
}

// TestSQLiteRepo_ListAll_AcrossUsers ListAll 必须返回所有用户的行（探活用）。
func TestSQLiteRepo_ListAll_AcrossUsers(t *testing.T) {
	repo := NewSQLiteRepo(newTestDB(t))
	_ = repo.Create(newTab(1, "A1"))
	_ = repo.Create(newTab(1, "A2"))
	_ = repo.Create(newTab(2, "B1"))

	all, err := repo.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListAll size = %d; want 3", len(all))
	}
}

// TestNewUUIDv4 生成的 UUID 格式正确、不重复。
func TestNewUUIDv4(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := newUUIDv4()
		if err != nil {
			t.Fatalf("newUUIDv4: %v", err)
		}
		if len(id) != 36 {
			t.Errorf("UUID length = %d; want 36", len(id))
		}
		if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			t.Errorf("UUID dashes wrong: %s", id)
		}
		if id[14] != '4' {
			t.Errorf("UUID version wrong: %s (want v4)", id)
		}
		if id == "default" {
			t.Errorf("UUID collided with reserved 'default'")
		}
		if seen[id] {
			t.Errorf("UUID collision: %s", id)
		}
		seen[id] = true
	}
}
