package auth

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/storage"

	_ "modernc.org/sqlite"
)

// newTestStorage 创建测试用 Storage。与 storage 包内测试的 helper 类似。
func newTestStorage(t *testing.T) (*storage.Storage, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfg := &config.Config{}
	cfg.Storage.DBPath = dbPath
	cfg.Storage.RetentionDays = 7

	// 绕过 storage.New 的后台 goroutine
	s, err := storage.New(cfg)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}

// 确保 storage.New 确实能被我们调用（验证 schema 可用）
func TestEnsureDefaultAdmin_CreatesOnEmpty(t *testing.T) {
	s, dataDir := newTestStorage(t)

	if err := EnsureDefaultAdmin(s, BcryptDefaultCost, dataDir); err != nil {
		t.Fatalf("EnsureDefaultAdmin: %v", err)
	}

	u, err := s.GetUserByUsername(DefaultAdminUsername)
	if err != nil {
		t.Fatalf("admin should exist: %v", err)
	}
	if !u.MustChangePassword {
		t.Fatalf("must_change should be 1 for new admin")
	}
	if u.Role != "admin" {
		t.Fatalf("role = %s, want admin", u.Role)
	}
	// 文件应存在
	if !InitialPasswordFileExists(dataDir) {
		t.Fatalf("initial-admin-password file should exist")
	}
	// 能用文件里的明文密码 bcrypt 校验到数据库里的哈希
	plain, err := os.ReadFile(InitialPasswordFilePath(dataDir))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	// 文件末尾有换行
	pwd := string(plain[:len(plain)-1])
	if !ComparePassword(u.PasswordHash, pwd) {
		t.Fatalf("bcrypt hash doesn't match written file password")
	}
}

func TestEnsureDefaultAdmin_Idempotent(t *testing.T) {
	s, dataDir := newTestStorage(t)

	if err := EnsureDefaultAdmin(s, BcryptDefaultCost, dataDir); err != nil {
		t.Fatalf("first: %v", err)
	}
	first, _ := s.GetUserByUsername(DefaultAdminUsername)

	// 第二次调用应 no-op
	if err := EnsureDefaultAdmin(s, BcryptDefaultCost, dataDir); err != nil {
		t.Fatalf("second: %v", err)
	}
	second, _ := s.GetUserByUsername(DefaultAdminUsername)
	if first.PasswordHash != second.PasswordHash {
		t.Fatalf("EnsureDefaultAdmin must not overwrite existing user")
	}
}

func TestEnsureDefaultAdmin_FileWriteFailureRollback(t *testing.T) {
	s, dataDir := newTestStorage(t)

	// 为触发写文件失败，将 dataDir 设为一个只读的不可写路径
	// 策略：把 dataDir 改成一个具体文件（os.OpenFile 写入时会报错）
	blockerFile := filepath.Join(dataDir, "blocker_not_dir")
	if err := os.WriteFile(blockerFile, []byte("x"), 0600); err != nil {
		t.Fatalf("setup blocker: %v", err)
	}
	// 使用 blockerFile 作为 dataDir（initialPasswordFilePath 会尝试在"文件"下创建子文件，失败）

	if err := EnsureDefaultAdmin(s, BcryptDefaultCost, blockerFile); err == nil {
		t.Fatalf("expected write failure, got nil")
	}

	// 回滚：users 表不应留下 admin
	n, _ := s.CountUsers()
	if n != 0 {
		t.Fatalf("rollback failed, user count = %d", n)
	}
	_, err := s.GetUserByUsername(DefaultAdminUsername)
	if err == nil {
		t.Fatalf("admin should be rolled back")
	}
}

// 保险：确保 storage_test 里的 helper 不是唯一能创建表的入口
var _ = sql.Open // keep import
