package tabs

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Tab 表示一个用户创建的远程 etcd 集群 Tab（不含默认 Tab——默认 Tab 由 cfg 派生不入库）。
type Tab struct {
	ID                string
	CreatedByUserID   int64
	Name              string
	Endpoint          string
	Username          string
	PasswordCipher    []byte // AES-GCM 密文（含 nonce）；空切片表示无密码
	SortOrder         int
	LastStatus        string // "ok" / "error" / "unknown"
	LastError         string
	LastCheckedAt     int64 // Unix 秒；0 表示从未探活
	CreatedAt         int64
	UpdatedAt         int64
}

// PatchFields 用于 Repo.UpdateByUser 的部分字段更新。
// 指针 nil 表示"不更新该字段"；非 nil 时使用其值（即使是空字符串）。
// PasswordCipher 用 *[]byte 区分"不修改"（nil）与"显式设为空"（&[]byte{}）。
type PatchFields struct {
	Name           *string
	Endpoint       *string
	Username       *string
	PasswordCipher *[]byte
	UpdatedAt      *int64
}

// 仓储错误。
var (
	// ErrTabNotFound Tab 不存在或不归属当前用户。
	// 注意：跨用户访问也返这个错误，避免存在性侧信道。
	ErrTabNotFound = errors.New("KV_TAB_NOT_FOUND")

	// ErrUserIDRequired 创建时 CreatedByUserID 必须 > 0。
	ErrUserIDRequired = errors.New("KV_TAB_USER_ID_REQUIRED")

	// ErrOrderMismatch 排序请求的 ID 数量与 DB 不一致。
	ErrOrderMismatch = errors.New("KV_TAB_ORDER_MISMATCH")
)

// Repo 抽象 Tab 持久化；所有"按 user_id 隔离"的方法都强制过滤会话用户。
//
// 设计：
//   - 业务接口（List/Get/Create/Update/UpdateOrder/Delete/Count）都带 userID，跨用户返 ErrTabNotFound
//   - 后台探活专用接口（UpdateStatus/ListAll）不带 userID——探活与会话身份正交
type Repo interface {
	// ListByUser 返回某用户的全部非默认 Tab，按 sort_order 升序。
	ListByUser(userID int64) ([]Tab, error)

	// Get 仅在 Tab 归属指定用户时返回；否则返 ErrTabNotFound。
	Get(id string, userID int64) (*Tab, error)

	// Create 新建 Tab；自动分配 UUID 主键（与默认 ID "default" 必然不冲突）。
	// CreatedByUserID 必须 > 0；否则返 ErrUserIDRequired。
	// 入参 SortOrder 会被忽略，自动分配为该用户当前最大 sort_order + 1。
	Create(t *Tab) error

	// UpdateByUser 部分字段更新；仅 fields 中非 nil 字段被写入。
	// 跨用户 / 不存在均返 ErrTabNotFound。
	UpdateByUser(id string, userID int64, fields PatchFields) error

	// UpdateOrderByUser 在事务内重写当前用户的所有 Tab 的 sort_order。
	// orderedIDs 必须恰好与 DB 中该用户的 Tab ID 集合相等（不计默认 Tab）；
	// 数量不一致或包含他人 / 不存在 ID 都返 ErrOrderMismatch / ErrTabNotFound。
	UpdateOrderByUser(userID int64, orderedIDs []string) error

	// UpdateStatus 后台探活专用——不带 userID。
	// 写入 last_status / last_error / last_checked_at；不更新 updated_at。
	UpdateStatus(id, status, errMsg string, checkedAt int64) error

	// DeleteByUser 删除归属用户的 Tab；跨用户 / 不存在返 ErrTabNotFound。
	DeleteByUser(id string, userID int64) error

	// CountByUser 返回当前用户的非默认 Tab 数（用于 per-user 限额）。
	CountByUser(userID int64) (int, error)

	// ListAll 返回 DB 中全部非默认 Tab——后台探活专用，不分用户。
	ListAll() ([]Tab, error)
}

// SQLiteRepo 是 Repo 的 SQLite 实现。
type SQLiteRepo struct {
	db *sql.DB
}

// NewSQLiteRepo 用一个已初始化的 *sql.DB 构造 Repo。
//
// 表与索引由 storage.initTables 创建，本构造函数不再自建表。
func NewSQLiteRepo(db *sql.DB) *SQLiteRepo {
	return &SQLiteRepo{db: db}
}

// ListByUser 实现 Repo.ListByUser。
func (r *SQLiteRepo) ListByUser(userID int64) ([]Tab, error) {
	rows, err := r.db.Query(`
		SELECT id, created_by_user_id, name, endpoint, username, password_cipher,
		       sort_order, last_status, last_error, last_checked_at, created_at, updated_at
		FROM kv_cluster_tabs
		WHERE created_by_user_id = ?
		ORDER BY sort_order ASC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list tabs: %w", err)
	}
	defer rows.Close()

	var tabs []Tab
	for rows.Next() {
		var t Tab
		if err := scanRow(rows, &t); err != nil {
			return nil, err
		}
		tabs = append(tabs, t)
	}
	return tabs, rows.Err()
}

// Get 实现 Repo.Get。
func (r *SQLiteRepo) Get(id string, userID int64) (*Tab, error) {
	row := r.db.QueryRow(`
		SELECT id, created_by_user_id, name, endpoint, username, password_cipher,
		       sort_order, last_status, last_error, last_checked_at, created_at, updated_at
		FROM kv_cluster_tabs
		WHERE id = ? AND created_by_user_id = ?
	`, id, userID)

	var t Tab
	if err := scanRow(row, &t); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTabNotFound
		}
		return nil, err
	}
	return &t, nil
}

// Create 实现 Repo.Create。
func (r *SQLiteRepo) Create(t *Tab) error {
	if t == nil {
		return fmt.Errorf("nil tab")
	}
	if t.CreatedByUserID <= 0 {
		return ErrUserIDRequired
	}

	id, err := newUUIDv4()
	if err != nil {
		return fmt.Errorf("generate UUID: %w", err)
	}
	t.ID = id

	// 分配 sort_order = 当前用户最大值 + 1（默认 0 起）
	var maxOrder sql.NullInt64
	if err := r.db.QueryRow(
		`SELECT COALESCE(MAX(sort_order), -1) FROM kv_cluster_tabs WHERE created_by_user_id = ?`,
		t.CreatedByUserID,
	).Scan(&maxOrder); err != nil {
		return fmt.Errorf("query max sort_order: %w", err)
	}
	t.SortOrder = int(maxOrder.Int64) + 1

	now := time.Now().Unix()
	if t.CreatedAt == 0 {
		t.CreatedAt = now
	}
	if t.UpdatedAt == 0 {
		t.UpdatedAt = now
	}
	if t.LastStatus == "" {
		t.LastStatus = "unknown"
	}

	_, err = r.db.Exec(`
		INSERT INTO kv_cluster_tabs
			(id, created_by_user_id, name, endpoint, username, password_cipher,
			 sort_order, last_status, last_error, last_checked_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		t.ID, t.CreatedByUserID, t.Name, t.Endpoint, t.Username, t.PasswordCipher,
		t.SortOrder, t.LastStatus, t.LastError, t.LastCheckedAt,
		t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert tab: %w", err)
	}
	return nil
}

// UpdateByUser 实现 Repo.UpdateByUser。
func (r *SQLiteRepo) UpdateByUser(id string, userID int64, fields PatchFields) error {
	// 拼装动态 UPDATE：仅更新 fields 中非 nil 字段
	sets := []string{}
	args := []interface{}{}

	if fields.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *fields.Name)
	}
	if fields.Endpoint != nil {
		sets = append(sets, "endpoint = ?")
		args = append(args, *fields.Endpoint)
	}
	if fields.Username != nil {
		sets = append(sets, "username = ?")
		args = append(args, *fields.Username)
	}
	if fields.PasswordCipher != nil {
		sets = append(sets, "password_cipher = ?")
		args = append(args, *fields.PasswordCipher)
	}

	// updated_at 总是更新
	updatedAt := time.Now().Unix()
	if fields.UpdatedAt != nil {
		updatedAt = *fields.UpdatedAt
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, updatedAt)

	if len(sets) == 1 {
		// 只有 updated_at——仍然执行（视为"touch"）但很奇怪；返回 nil 但什么都不做？
		// 选择：执行更新以保持语义一致（更新 updated_at 也算"修改"）
	}

	args = append(args, id, userID)
	query := fmt.Sprintf(`UPDATE kv_cluster_tabs SET %s WHERE id = ? AND created_by_user_id = ?`,
		joinComma(sets))
	res, err := r.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("update tab: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTabNotFound
	}
	return nil
}

// UpdateOrderByUser 实现 Repo.UpdateOrderByUser。
func (r *SQLiteRepo) UpdateOrderByUser(userID int64, orderedIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 校验数量与归属
	var actual int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM kv_cluster_tabs WHERE created_by_user_id = ?`, userID,
	).Scan(&actual); err != nil {
		return fmt.Errorf("count tabs: %w", err)
	}
	if actual != len(orderedIDs) {
		return ErrOrderMismatch
	}

	// 逐条 UPDATE；任一返 0 行则视为他人 ID 或不存在 → ErrTabNotFound（不暴露存在性）
	stmt, err := tx.Prepare(
		`UPDATE kv_cluster_tabs SET sort_order = ?, updated_at = ?
		 WHERE id = ? AND created_by_user_id = ?`)
	if err != nil {
		return fmt.Errorf("prepare update: %w", err)
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for i, id := range orderedIDs {
		res, err := stmt.Exec(i, now, id, userID)
		if err != nil {
			return fmt.Errorf("update sort_order: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrTabNotFound
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// UpdateStatus 实现 Repo.UpdateStatus（后台探活专用，不带 userID）。
func (r *SQLiteRepo) UpdateStatus(id, status, errMsg string, checkedAt int64) error {
	_, err := r.db.Exec(
		`UPDATE kv_cluster_tabs
		 SET last_status = ?, last_error = ?, last_checked_at = ?
		 WHERE id = ?`,
		status, errMsg, checkedAt, id,
	)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	// 不返 ErrTabNotFound——探活时 Tab 可能刚被删除，写不到行是正常情况
	return nil
}

// DeleteByUser 实现 Repo.DeleteByUser。
func (r *SQLiteRepo) DeleteByUser(id string, userID int64) error {
	res, err := r.db.Exec(
		`DELETE FROM kv_cluster_tabs WHERE id = ? AND created_by_user_id = ?`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("delete tab: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTabNotFound
	}
	return nil
}

// CountByUser 实现 Repo.CountByUser。
func (r *SQLiteRepo) CountByUser(userID int64) (int, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM kv_cluster_tabs WHERE created_by_user_id = ?`, userID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count tabs: %w", err)
	}
	return n, nil
}

// ListAll 实现 Repo.ListAll（后台探活专用）。
func (r *SQLiteRepo) ListAll() ([]Tab, error) {
	rows, err := r.db.Query(`
		SELECT id, created_by_user_id, name, endpoint, username, password_cipher,
		       sort_order, last_status, last_error, last_checked_at, created_at, updated_at
		FROM kv_cluster_tabs
	`)
	if err != nil {
		return nil, fmt.Errorf("list all tabs: %w", err)
	}
	defer rows.Close()

	var tabs []Tab
	for rows.Next() {
		var t Tab
		if err := scanRow(rows, &t); err != nil {
			return nil, err
		}
		tabs = append(tabs, t)
	}
	return tabs, rows.Err()
}

// scanRow 抽象 *sql.Row 与 *sql.Rows 的统一扫描。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(s rowScanner, t *Tab) error {
	return s.Scan(
		&t.ID, &t.CreatedByUserID, &t.Name, &t.Endpoint, &t.Username, &t.PasswordCipher,
		&t.SortOrder, &t.LastStatus, &t.LastError, &t.LastCheckedAt,
		&t.CreatedAt, &t.UpdatedAt,
	)
}

// joinComma 用 ", " 连接字符串切片（避免引入 strings 仅为一个 Join）。
func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// newUUIDv4 生成 RFC4122 UUID v4 字符串（标准库自实现，避免新增依赖）。
//
// 格式：xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
// 结果与默认 Tab id "default" 必然不冲突（包含连字符且固定长度 36）。
func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	// version 4
	b[6] = (b[6] & 0x0F) | 0x40
	// variant RFC 4122
	b[8] = (b[8] & 0x3F) | 0x80
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	for i, j := 0, 0; i < 16; i++ {
		// 在 4-、6-、8-、10- 字节后插入连字符
		if j == 8 || j == 13 || j == 18 || j == 23 {
			out[j] = '-'
			j++
		}
		out[j] = hex[b[i]>>4]
		out[j+1] = hex[b[i]&0x0F]
		j += 2
	}
	return string(out), nil
}
