package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"etcdmonitor/internal/config"

	_ "modernc.org/sqlite"
)

// newTestStorage 创建测试用的 Storage 实例，绕过 New() 避免后台 goroutine
func newTestStorage(t *testing.T, endpoint string) *Storage {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	cfg := &config.Config{}
	cfg.Etcd.Endpoint = endpoint
	cfg.Storage.DBPath = dbPath
	cfg.Storage.RetentionDays = 7

	s := &Storage{cfg: cfg}

	// 手动打开数据库
	db, err := openTestDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	s.db = db

	if err := s.initTables(); err != nil {
		t.Fatalf("init tables: %v", err)
	}
	s.migrateSchema()

	t.Cleanup(func() {
		s.db.Close()
	})

	return s
}

// openDB 从 dbPath 打开 SQLite 数据库
func openTestDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

func TestStore_AndQueryLatest(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	ts := time.Now()
	metrics := map[string]float64{
		"cpu_usage":    0.45,
		"memory_bytes": 52428800,
		"goroutines":   42,
	}

	if err := s.Store(ts, "member1", metrics); err != nil {
		t.Fatalf("Store() error: %v", err)
	}

	// 写入第二批，goroutines 更新
	ts2 := ts.Add(30 * time.Second)
	metrics2 := map[string]float64{
		"goroutines": 50,
	}
	if err := s.Store(ts2, "member1", metrics2); err != nil {
		t.Fatalf("Store() error: %v", err)
	}

	result, err := s.QueryLatest("member1", []string{"cpu_usage", "memory_bytes", "goroutines"})
	if err != nil {
		t.Fatalf("QueryLatest() error: %v", err)
	}

	if result["cpu_usage"] != 0.45 {
		t.Errorf("cpu_usage = %f, want 0.45", result["cpu_usage"])
	}
	if result["goroutines"] != 50 {
		t.Errorf("goroutines = %f, want 50 (latest)", result["goroutines"])
	}
}

func TestStore_EmptyMetrics(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	err := s.Store(time.Now(), "member1", map[string]float64{})
	if err != nil {
		t.Errorf("Store() with empty metrics should not error: %v", err)
	}
}

func TestStore_SkipsNameSuffix(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	metrics := map[string]float64{
		"grpc_method_0_name":  0,
		"grpc_method_0_count": 50000,
		"normal_metric":       42,
	}

	if err := s.Store(time.Now(), "member1", metrics); err != nil {
		t.Fatalf("Store() error: %v", err)
	}

	result, err := s.QueryLatest("member1", []string{"grpc_method_0_name", "grpc_method_0_count", "normal_metric"})
	if err != nil {
		t.Fatalf("QueryLatest() error: %v", err)
	}

	// _name 后缀的指标不应被存储
	if _, ok := result["grpc_method_0_name"]; ok {
		t.Error("_name metric should not be stored")
	}
	if result["grpc_method_0_count"] != 50000 {
		t.Errorf("count = %f, want 50000", result["grpc_method_0_count"])
	}
	if result["normal_metric"] != 42 {
		t.Errorf("normal = %f, want 42", result["normal_metric"])
	}
}

func TestQueryRange(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	base := time.Now().Add(-10 * time.Minute)

	// 写入 10 个数据点，每分钟一个
	for i := 0; i < 10; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		metrics := map[string]float64{
			"cpu": float64(i) * 10,
		}
		if err := s.Store(ts, "member1", metrics); err != nil {
			t.Fatalf("Store() error: %v", err)
		}
	}

	// 查询全部范围（< 30 分钟，不降采样）
	result, err := s.QueryRange("member1", []string{"cpu"}, base, base.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("QueryRange() error: %v", err)
	}

	points := result["cpu"]
	if len(points) != 10 {
		t.Fatalf("expected 10 data points, got %d", len(points))
	}

	// 验证升序
	for i := 1; i < len(points); i++ {
		if points[i].Timestamp < points[i-1].Timestamp {
			t.Error("data points not in ascending order")
			break
		}
	}
}

func TestQueryRange_EmptyMetrics(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	result, err := s.QueryRange("member1", []string{}, time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("QueryRange() error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for empty metrics, got %v", result)
	}
}

func TestQueryLatest_EmptyMetrics(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	result, err := s.QueryLatest("member1", []string{})
	if err != nil {
		t.Fatalf("QueryLatest() error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for empty metrics, got %v", result)
	}
}

func TestCheckEndpointChange_FirstRun(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	if err := s.CheckEndpointChange(); err != nil {
		t.Fatalf("CheckEndpointChange() first run error: %v", err)
	}

	// 第二次调用，endpoint 不变
	if err := s.CheckEndpointChange(); err != nil {
		t.Fatalf("CheckEndpointChange() second run error: %v", err)
	}
}

func TestCheckEndpointChange_EndpointChanged(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	// 首次运行
	if err := s.CheckEndpointChange(); err != nil {
		t.Fatalf("first CheckEndpointChange() error: %v", err)
	}

	// 写入一些数据
	if err := s.Store(time.Now(), "member1", map[string]float64{"cpu": 42}); err != nil {
		t.Fatalf("Store() error: %v", err)
	}

	// 验证数据存在
	result, err := s.QueryLatest("member1", []string{"cpu"})
	if err != nil {
		t.Fatalf("QueryLatest() error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected data before endpoint change")
	}

	// 切换 endpoint
	s.cfg.Etcd.Endpoint = "http://10.0.1.1:2379"
	if err := s.CheckEndpointChange(); err != nil {
		t.Fatalf("CheckEndpointChange() after change error: %v", err)
	}

	// 数据应该被清空
	result, err = s.QueryLatest("member1", []string{"cpu"})
	if err != nil {
		t.Fatalf("QueryLatest() after cleanup error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty data after endpoint change, got %v", result)
	}
}

func TestStoreAuditLog(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	entry := AuditEntry{
		Timestamp:  time.Now().Unix(),
		Username:   "admin",
		Operation:  "defragment",
		Target:     "member1",
		Params:     `{"member_id":"abc123"}`,
		Result:     "success",
		DurationMs: 1500,
		Success:    true,
	}

	if err := s.StoreAuditLog(entry); err != nil {
		t.Fatalf("StoreAuditLog() error: %v", err)
	}

	// 查询验证
	entries, total, err := s.QueryAuditLogs(AuditFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("QueryAuditLogs() error: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total=1, got %d", total)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Username != "admin" {
		t.Errorf("username = %q, want %q", entries[0].Username, "admin")
	}
	if entries[0].Operation != "defragment" {
		t.Errorf("operation = %q, want %q", entries[0].Operation, "defragment")
	}
	if !entries[0].Success {
		t.Error("expected success=true")
	}
}

func TestQueryAuditLogs_FilterByOperation(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	now := time.Now().Unix()
	for _, op := range []string{"defragment", "snapshot", "defragment", "move_leader"} {
		if err := s.StoreAuditLog(AuditEntry{
			Timestamp: now, Username: "admin", Operation: op, Success: true,
		}); err != nil {
			t.Fatalf("StoreAuditLog() error: %v", err)
		}
	}

	// 筛选 defragment
	entries, total, err := s.QueryAuditLogs(AuditFilter{Operation: "defragment", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("QueryAuditLogs() error: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2 for defragment, got %d", total)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestQueryAuditLogs_Pagination(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	now := time.Now().Unix()
	for i := 0; i < 5; i++ {
		if err := s.StoreAuditLog(AuditEntry{
			Timestamp: now + int64(i), Username: "admin", Operation: "defragment", Success: true,
		}); err != nil {
			t.Fatalf("StoreAuditLog() error: %v", err)
		}
	}

	// 第一页，每页 2 条
	entries, total, err := s.QueryAuditLogs(AuditFilter{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("QueryAuditLogs() error: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries on page 1, got %d", len(entries))
	}

	// 第三页，应该只有 1 条
	entries, _, err = s.QueryAuditLogs(AuditFilter{Page: 3, PageSize: 2})
	if err != nil {
		t.Fatalf("QueryAuditLogs() error: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry on page 3, got %d", len(entries))
	}
}

func TestQueryAuditLogs_TimeDescOrder(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	for i := 0; i < 3; i++ {
		if err := s.StoreAuditLog(AuditEntry{
			Timestamp: int64(1000 + i), Username: "admin", Operation: "test", Success: true,
		}); err != nil {
			t.Fatalf("StoreAuditLog() error: %v", err)
		}
	}

	entries, _, err := s.QueryAuditLogs(AuditFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("QueryAuditLogs() error: %v", err)
	}
	// 应该按时间倒序
	for i := 1; i < len(entries); i++ {
		if entries[i].Timestamp > entries[i-1].Timestamp {
			t.Error("entries not in descending timestamp order")
			break
		}
	}
}

func TestCheckEndpointChange_AuditLogPreserved(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	if err := s.CheckEndpointChange(); err != nil {
		t.Fatalf("first CheckEndpointChange() error: %v", err)
	}

	// 写入审计日志
	if err := s.StoreAuditLog(AuditEntry{
		Timestamp: time.Now().Unix(), Username: "admin", Operation: "defragment", Success: true,
	}); err != nil {
		t.Fatalf("StoreAuditLog() error: %v", err)
	}

	// 切换 endpoint（触发 metrics 清理）
	s.cfg.Etcd.Endpoint = "http://10.0.1.1:2379"
	if err := s.CheckEndpointChange(); err != nil {
		t.Fatalf("CheckEndpointChange() error: %v", err)
	}

	// 审计日志应该保留
	entries, total, err := s.QueryAuditLogs(AuditFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("QueryAuditLogs() error: %v", err)
	}
	if total != 1 || len(entries) != 1 {
		t.Errorf("audit log should be preserved after endpoint change, got total=%d entries=%d", total, len(entries))
	}
}
