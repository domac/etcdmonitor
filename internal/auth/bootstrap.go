package auth

import (
	"fmt"

	"etcdmonitor/internal/logger"
	"etcdmonitor/internal/storage"
)

// DefaultAdminUsername 初始管理员账号的用户名（固定）
const DefaultAdminUsername = "admin"

// EnsureDefaultAdmin 在 users 表为空时自动创建初始 admin 账号：
//  1. 生成 16 字符随机密码（crypto/rand + base62 无歧义字符）
//  2. bcrypt 写入 users 表，must_change_password=1，role=admin
//  3. 明文密码写入 <dataDir>/initial-admin-password 文件 (mode 0600)
//  4. 打印 WARN 日志（只含文件路径与改密提示，禁止输出明文密码）
//
// 已存在任意用户时直接返回 nil（不覆盖已有账号，也不重建文件）。
func EnsureDefaultAdmin(repo *storage.Storage, bcryptCost int, dataDir string) error {
	n, err := repo.CountUsers()
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		return nil
	}

	plain, err := GenerateInitialPassword()
	if err != nil {
		return fmt.Errorf("generate initial password: %w", err)
	}
	hash, err := HashPassword(plain, bcryptCost)
	if err != nil {
		return fmt.Errorf("hash initial password: %w", err)
	}

	// 先写 users 表；若随后写文件失败则回滚用户插入，避免"有账号但没人知道密码"
	if err := repo.CreateUser(&storage.User{
		Username:           DefaultAdminUsername,
		PasswordHash:       hash,
		Role:               "admin",
		MustChangePassword: true,
	}); err != nil {
		return fmt.Errorf("create default admin: %w", err)
	}

	if err := WriteInitialPasswordFile(dataDir, plain); err != nil {
		// 回滚 users 表
		if rollbackErr := repo.DeleteUser(DefaultAdminUsername); rollbackErr != nil {
			logger.Errorf("[Auth] CRITICAL: rollback of default admin failed after initial password file write failure: %v", rollbackErr)
		}
		return fmt.Errorf("write initial password file: %w", err)
	}

	path := InitialPasswordFilePath(dataDir)
	logger.Warnf("[Auth] ===============================================================")
	logger.Warnf("[Auth]  Default admin account created.")
	logger.Warnf("[Auth]  Initial password saved to:")
	logger.Warnf("[Auth]    %s", path)
	logger.Warnf("[Auth]  Please login with username 'admin' and change the password")
	logger.Warnf("[Auth]  immediately. The file will be deleted automatically after a")
	logger.Warnf("[Auth]  successful first password change.")
	logger.Warnf("[Auth] ===============================================================")
	return nil
}
