package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/logger"

	_ "modernc.org/sqlite"
)

// DataPoint 时序数据点
type DataPoint struct {
	Timestamp int64   `json:"ts"`
	Value     float64 `json:"value"`
}

// Storage 使用 SQLite 存储时序指标数据
type Storage struct {
	db  *sql.DB
	cfg *config.Config
}

// New 创建并初始化存储
func New(cfg *config.Config) (*Storage, error) {
	dbDir := filepath.Dir(cfg.Storage.DBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := sql.Open("sqlite", cfg.Storage.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-64000",
		"PRAGMA busy_timeout=5000",
		"PRAGMA temp_store=MEMORY",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			logger.Warnf("[Storage] Warning: %s failed: %v", p, err)
		}
	}

	// Configure connection pool for SQLite
	// SQLite works best with a single connection due to write-locking behavior
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(10 * time.Minute)

	s := &Storage{db: db, cfg: cfg}

	if err := s.initTables(); err != nil {
		return nil, fmt.Errorf("init tables: %w", err)
	}

	s.migrateSchema()

	if err := s.CheckEndpointChange(); err != nil {
		return nil, fmt.Errorf("check endpoint: %w", err)
	}

	go s.retentionLoop()

	return s, nil
}

func (s *Storage) initTables() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			member_id TEXT NOT NULL DEFAULT '',
			metric_name TEXT NOT NULL,
			metric_value REAL NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_metrics_member_ts ON metrics(member_id, timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_member_name_ts ON metrics(member_id, metric_name, timestamp);
		CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS ops_audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			username TEXT NOT NULL DEFAULT '',
			operation TEXT NOT NULL,
			target TEXT NOT NULL DEFAULT '',
			params TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			success INTEGER NOT NULL DEFAULT 1
		);
		CREATE INDEX IF NOT EXISTS idx_audit_ts ON ops_audit_log(timestamp);
		CREATE INDEX IF NOT EXISTS idx_audit_operation ON ops_audit_log(operation);
	`)
	return err
}

func (s *Storage) migrateSchema() {
	rows, err := s.db.Query("PRAGMA table_info(metrics)")
	if err != nil {
		return
	}
	defer rows.Close()

	hasMemberID := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			continue
		}
		if name == "member_id" {
			hasMemberID = true
		}
	}

	if !hasMemberID {
		logger.Info("[Storage] Migrating schema: adding member_id column...")
		_, err := s.db.Exec("ALTER TABLE metrics ADD COLUMN member_id TEXT NOT NULL DEFAULT ''")
		if err != nil {
			logger.Warnf("[Storage] Migration warning: %v", err)
		} else {
			if _, err := s.db.Exec("DROP INDEX IF EXISTS idx_metrics_ts"); err != nil {
				logger.Warnf("[Storage] Drop index warning: %v", err)
			}
			if _, err := s.db.Exec("DROP INDEX IF EXISTS idx_metrics_name_ts"); err != nil {
				logger.Warnf("[Storage] Drop index warning: %v", err)
			}
			if _, err := s.db.Exec("CREATE INDEX IF NOT EXISTS idx_metrics_member_ts ON metrics(member_id, timestamp)"); err != nil {
				logger.Warnf("[Storage] Create index warning: %v", err)
			}
			if _, err := s.db.Exec("CREATE INDEX IF NOT EXISTS idx_metrics_member_name_ts ON metrics(member_id, metric_name, timestamp)"); err != nil {
				logger.Warnf("[Storage] Create index warning: %v", err)
			}
			logger.Info("[Storage] Migration complete")
		}
	}
}

// CheckEndpointChange 检查 etcd 地址是否变更，变更则清理所有监控数据
func (s *Storage) CheckEndpointChange() error {
	currentEndpoint := config.NormalizeEndpoint(s.cfg.EtcdFirstEndpoint())

	var lastEndpoint string
	err := s.db.QueryRow("SELECT value FROM meta WHERE key = 'etcd_endpoint'").Scan(&lastEndpoint)

	if err == sql.ErrNoRows {
		_, err = s.db.Exec("INSERT INTO meta (key, value) VALUES ('etcd_endpoint', ?)", currentEndpoint)
		if err != nil {
			return fmt.Errorf("save endpoint: %w", err)
		}
		logger.Infof("[Storage] First run, recording etcd endpoint: %s", currentEndpoint)
		return nil
	}
	if err != nil {
		return fmt.Errorf("read endpoint: %w", err)
	}

	if config.NormalizeEndpoint(lastEndpoint) == currentEndpoint {
		logger.Infof("[Storage] etcd endpoint unchanged: %s", currentEndpoint)
		return nil
	}

	logger.Warnf("[Storage] *** etcd endpoint changed: %s -> %s ***", lastEndpoint, currentEndpoint)
	logger.Infof("[Storage] Cleaning all historical monitoring data to prevent data mixing...")

	result, err := s.db.Exec("DELETE FROM metrics")
	if err != nil {
		return fmt.Errorf("cleanup metrics: %w", err)
	}
	rows, _ := result.RowsAffected()
	logger.Infof("[Storage] Cleaned %d records", rows)

	_, _ = s.db.Exec("VACUUM") // best-effort reclaim after data cleanup

	_, err = s.db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('etcd_endpoint', ?)", currentEndpoint)
	if err != nil {
		return fmt.Errorf("update endpoint: %w", err)
	}

	logger.Infof("[Storage] Endpoint updated, starting fresh data collection")
	return nil
}

// Store 批量存储一个成员的一组指标
func (s *Storage) Store(ts time.Time, memberID string, metrics map[string]float64) error {
	if len(metrics) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT INTO metrics (timestamp, member_id, metric_name, metric_value) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	tsUnix := ts.Unix()
	for name, value := range metrics {
		if strings.HasSuffix(name, "_name") {
			continue
		}
		if _, err := stmt.Exec(tsUnix, memberID, name, value); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// QueryRange 查询指定成员的指标在时间范围内的数据
// 自动降采样：时间范围越大，聚合粒度越粗，控制返回的数据点数量
func (s *Storage) QueryRange(memberID string, metricNames []string, start, end time.Time) (map[string][]DataPoint, error) {
	if len(metricNames) == 0 {
		return nil, nil
	}

	duration := end.Sub(start)

	// 降采样策略：根据时间范围决定聚合间隔
	// 目标：每个指标最多返回约 360 个数据点，保证前端流畅
	var groupInterval int64
	switch {
	case duration <= 30*time.Minute:
		groupInterval = 0 // 不聚合，返回原始数据
	case duration <= 2*time.Hour:
		groupInterval = 30 // 30秒一个点
	case duration <= 12*time.Hour:
		groupInterval = 120 // 2分钟一个点
	case duration <= 48*time.Hour:
		groupInterval = 300 // 5分钟一个点
	default:
		groupInterval = 600 // 10分钟一个点
	}

	placeholders := make([]string, len(metricNames))
	args := make([]interface{}, 0, len(metricNames)+3)
	args = append(args, memberID, start.Unix(), end.Unix())
	for i, name := range metricNames {
		placeholders[i] = "?"
		args = append(args, name)
	}

	var query string
	if groupInterval == 0 {
		// 短时间范围：返回原始数据
		query = fmt.Sprintf(`
			SELECT timestamp, metric_name, metric_value
			FROM metrics
			WHERE member_id = ? AND timestamp >= ? AND timestamp <= ?
			  AND metric_name IN (%s)
			ORDER BY timestamp ASC
		`, strings.Join(placeholders, ","))
	} else {
		// 长时间范围：按时间窗口聚合取平均值
		query = fmt.Sprintf(`
			SELECT (timestamp / %d) * %d AS ts_group, metric_name, AVG(metric_value)
			FROM metrics
			WHERE member_id = ? AND timestamp >= ? AND timestamp <= ?
			  AND metric_name IN (%s)
			GROUP BY ts_group, metric_name
			ORDER BY ts_group ASC
		`, groupInterval, groupInterval, strings.Join(placeholders, ","))
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]DataPoint)
	for rows.Next() {
		var ts int64
		var name string
		var value float64
		if err := rows.Scan(&ts, &name, &value); err != nil {
			continue
		}
		result[name] = append(result[name], DataPoint{
			Timestamp: ts,
			Value:     value,
		})
	}

	return result, rows.Err()
}

// QueryLatest 查询指定成员每个指标的最新值
func (s *Storage) QueryLatest(memberID string, metricNames []string) (map[string]float64, error) {
	if len(metricNames) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(metricNames))
	args := make([]interface{}, 0, len(metricNames)+1)
	args = append(args, memberID)
	for i, name := range metricNames {
		placeholders[i] = "?"
		args = append(args, name)
	}

	query := fmt.Sprintf(`
		SELECT metric_name, metric_value
		FROM metrics
		WHERE id IN (
			SELECT MAX(id) FROM metrics
			WHERE member_id = ? AND metric_name IN (%s)
			GROUP BY metric_name
		)
	`, strings.Join(placeholders, ","))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]float64)
	for rows.Next() {
		var name string
		var value float64
		if err := rows.Scan(&name, &value); err != nil {
			continue
		}
		result[name] = value
	}

	return result, rows.Err()
}

func (s *Storage) retentionLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	s.cleanup()

	for range ticker.C {
		s.cleanup()
	}
}

func (s *Storage) cleanup() {
	retentionDays := s.cfg.Storage.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 7
	}

	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()

	result, err := s.db.Exec("DELETE FROM metrics WHERE timestamp < ?", cutoff)
	if err != nil {
		logger.Errorf("[Storage] Cleanup error: %v", err)
		return
	}

	if rows, _ := result.RowsAffected(); rows > 0 {
		logger.Infof("[Storage] Cleaned up %d expired records (older than %d days)", rows, retentionDays)
		// VACUUM 回收磁盘空间（大量删除后执行）
		if rows > 10000 {
			logger.Infof("[Storage] Running VACUUM to reclaim disk space...")
			if _, err := s.db.Exec("VACUUM"); err != nil {
				logger.Warnf("[Storage] VACUUM warning: %v", err)
			}
		} else {
			if _, err := s.db.Exec("PRAGMA incremental_vacuum"); err != nil {
				logger.Warnf("[Storage] Incremental vacuum warning: %v", err)
			}
		}
	}

	// 清理过期审计日志（独立保留策略，不受 etcd 地址变更影响）
	auditRetentionDays := s.cfg.Ops.AuditRetentionDays
	if auditRetentionDays <= 0 {
		auditRetentionDays = 7
	}
	auditCutoff := time.Now().Add(-time.Duration(auditRetentionDays) * 24 * time.Hour).Unix()
	auditResult, err := s.db.Exec("DELETE FROM ops_audit_log WHERE timestamp < ?", auditCutoff)
	if err != nil {
		logger.Errorf("[Storage] Audit log cleanup error: %v", err)
		return
	}
	if rows, _ := auditResult.RowsAffected(); rows > 0 {
		logger.Infof("[Storage] Cleaned up %d expired audit log records (older than %d days)", rows, auditRetentionDays)
	}
}

// Close 关闭数据库
func (s *Storage) Close() error {
	return s.db.Close()
}

// AuditEntry 审计日志条目
type AuditEntry struct {
	ID         int64  `json:"id"`
	Timestamp  int64  `json:"timestamp"`
	Username   string `json:"username"`
	Operation  string `json:"operation"`
	Target     string `json:"target"`
	Params     string `json:"params"`
	Result     string `json:"result"`
	DurationMs int64  `json:"duration_ms"`
	Success    bool   `json:"success"`
}

// AuditFilter 审计日志查询过滤条件
type AuditFilter struct {
	Operation string // 按操作类型筛选，空表示不筛选
	Page      int    // 页码，从 1 开始
	PageSize  int    // 每页条数，默认 20
}

// StoreAuditLog 写入一条审计日志
func (s *Storage) StoreAuditLog(entry AuditEntry) error {
	successInt := 0
	if entry.Success {
		successInt = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO ops_audit_log (timestamp, username, operation, target, params, result, duration_ms, success)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.Timestamp, entry.Username, entry.Operation, entry.Target, entry.Params, entry.Result, entry.DurationMs, successInt)
	return err
}

// QueryAuditLogs 分页查询审计日志，按时间倒序
func (s *Storage) QueryAuditLogs(filter AuditFilter) ([]AuditEntry, int64, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}

	// 构建 WHERE 子句
	where := ""
	var args []interface{}
	if filter.Operation != "" {
		where = " WHERE operation = ?"
		args = append(args, filter.Operation)
	}

	// 查询总数
	var total int64
	countQuery := "SELECT COUNT(*) FROM ops_audit_log" + where
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// 查询分页数据
	offset := (filter.Page - 1) * filter.PageSize
	dataQuery := "SELECT id, timestamp, username, operation, target, params, result, duration_ms, success FROM ops_audit_log" +
		where + " ORDER BY timestamp DESC, id DESC LIMIT ? OFFSET ?"
	dataArgs := append(args, filter.PageSize, offset)

	rows, err := s.db.Query(dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var successInt int
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Username, &e.Operation, &e.Target, &e.Params, &e.Result, &e.DurationMs, &successInt); err != nil {
			continue
		}
		e.Success = successInt == 1
		entries = append(entries, e)
	}

	return entries, total, rows.Err()
}

// DebugMemberIDs 调试用：返回数据库中所有不同的 member_id 及其记录数
func (s *Storage) DebugMemberIDs() map[string]int64 {
	result := make(map[string]int64)
	rows, err := s.db.Query("SELECT member_id, COUNT(*) FROM metrics GROUP BY member_id")
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var count int64
		if err := rows.Scan(&id, &count); err == nil {
			result[id] = count
		}
	}
	return result
}
