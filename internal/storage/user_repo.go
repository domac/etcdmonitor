package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// User 代表一个 Dashboard 本地账号记录
type User struct {
	ID                 int64
	Username           string
	PasswordHash       string
	Role               string
	MustChangePassword bool
	FailedAttempts     int
	LockedUntil        int64 // unix seconds；0 表示未锁定
	LastLoginAt        int64 // unix seconds
	CreatedAt          int64
	UpdatedAt          int64
}

// IsLocked 判断指定时刻此用户是否处于锁定中
func (u *User) IsLocked(now time.Time) bool {
	return u.LockedUntil > 0 && u.LockedUntil > now.Unix()
}

// ErrUserNotFound 用户不存在
var ErrUserNotFound = errors.New("user not found")

// CountUsers 返回 users 表中的用户数量
func (s *Storage) CountUsers() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// CreateUser 插入一条新用户记录
func (s *Storage) CreateUser(u *User) error {
	now := time.Now().Unix()
	u.CreatedAt = now
	u.UpdatedAt = now
	mustChange := 0
	if u.MustChangePassword {
		mustChange = 1
	}
	res, err := s.db.Exec(
		`INSERT INTO users
			(username, password_hash, role, must_change_password,
			 failed_attempts, locked_until, last_login_at,
			 created_at, updated_at)
		 VALUES (?, ?, ?, ?, 0, 0, 0, ?, ?)`,
		u.Username, u.PasswordHash, u.Role, mustChange, now, now,
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	if id, err := res.LastInsertId(); err == nil {
		u.ID = id
	}
	return nil
}

// DeleteUser 删除指定用户（供测试或极端恢复使用）
func (s *Storage) DeleteUser(username string) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE username = ?`, username)
	return err
}

// GetUserByUsername 读取用户；不存在返回 ErrUserNotFound
func (s *Storage) GetUserByUsername(username string) (*User, error) {
	u := &User{}
	var mustChange int
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, role, must_change_password,
				failed_attempts, locked_until, last_login_at, created_at, updated_at
		 FROM users WHERE username = ?`, username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &mustChange,
		&u.FailedAttempts, &u.LockedUntil, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	u.MustChangePassword = mustChange != 0
	return u, nil
}

// UpdatePassword 覆盖写密码哈希、must_change 与 updated_at，并清理锁定状态
func (s *Storage) UpdatePassword(username, newHash string, mustChange bool) error {
	now := time.Now().Unix()
	mc := 0
	if mustChange {
		mc = 1
	}
	res, err := s.db.Exec(
		`UPDATE users
		   SET password_hash = ?,
		       must_change_password = ?,
		       failed_attempts = 0,
		       locked_until = 0,
		       updated_at = ?
		 WHERE username = ?`,
		newHash, mc, now, username,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// IncrementFailedAttempts 仅 +1，不处理锁定（调用方判断阈值后调用 SetLocked）。
// 返回累加后的值。
func (s *Storage) IncrementFailedAttempts(username string) (int, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE users
		   SET failed_attempts = failed_attempts + 1,
		       updated_at = ?
		 WHERE username = ?`,
		now, username,
	)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return 0, ErrUserNotFound
	}
	var n int
	if err := s.db.QueryRow(`SELECT failed_attempts FROM users WHERE username = ?`, username).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// SetLocked 设置 locked_until 时间戳（秒），通常在 IncrementFailedAttempts 达到阈值后调用
func (s *Storage) SetLocked(username string, untilUnix int64) error {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE users
		   SET locked_until = ?,
		       updated_at = ?
		 WHERE username = ?`,
		untilUnix, now, username,
	)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ResetLoginState 将 failed_attempts / locked_until 重置为 0
func (s *Storage) ResetLoginState(username string) error {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE users
		   SET failed_attempts = 0,
		       locked_until = 0,
		       updated_at = ?
		 WHERE username = ?`,
		now, username,
	)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// UpdateLastLogin 更新 last_login_at（登录成功后调用）
func (s *Storage) UpdateLastLogin(username string) error {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE users
		   SET last_login_at = ?,
		       updated_at = ?
		 WHERE username = ?`,
		now, now, username,
	)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrUserNotFound
	}
	return nil
}
