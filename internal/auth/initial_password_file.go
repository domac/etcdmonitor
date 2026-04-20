package auth

import (
	"fmt"
	"os"
	"path/filepath"
)

// InitialPasswordFileName 初始管理员密码文件名（位于 data 目录下）
const InitialPasswordFileName = "initial-admin-password"

// InitialPasswordFilePath 返回初始密码文件的绝对路径
func InitialPasswordFilePath(dataDir string) string {
	return filepath.Join(dataDir, InitialPasswordFileName)
}

// WriteInitialPasswordFile 将明文密码写入 data/initial-admin-password 文件（mode 0600）。
// 文件内容为"密码 + 换行"。写入失败由调用方决定回滚策略。
func WriteInitialPasswordFile(dataDir, password string) error {
	path := InitialPasswordFilePath(dataDir)
	// 使用 O_CREATE|O_WRONLY|O_TRUNC 保证幂等覆盖；权限 0600 由 OpenFile 直接设定
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("open initial password file %s: %w", path, err)
	}
	defer f.Close()
	// 双保险：显式 Chmod（某些平台 umask 可能影响）
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("chmod initial password file: %w", err)
	}
	if _, err := f.WriteString(password + "\n"); err != nil {
		return fmt.Errorf("write initial password file: %w", err)
	}
	return nil
}

// InitialPasswordFileExists 返回 data/initial-admin-password 文件是否存在
func InitialPasswordFileExists(dataDir string) bool {
	_, err := os.Stat(InitialPasswordFilePath(dataDir))
	return err == nil
}

// DeleteInitialPasswordFile 删除初始密码文件。文件不存在视为删除成功。
func DeleteInitialPasswordFile(dataDir string) error {
	path := InitialPasswordFilePath(dataDir)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("remove initial password file: %w", err)
	}
	return nil
}
