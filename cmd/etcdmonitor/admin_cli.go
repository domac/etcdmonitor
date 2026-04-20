package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"etcdmonitor/internal/auth"
	"etcdmonitor/internal/config"
	"etcdmonitor/internal/logger"
	"etcdmonitor/internal/storage"

	"golang.org/x/term"
)

// runResetPassword 实现 `etcdmonitor reset-password` 子命令。
// 写入新 bcrypt 哈希，自动置 must_change_password=1，清锁。
func runResetPassword(args []string) error {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	configPath := fs.String("config", "config.yaml", "Path to config file")
	username := fs.String("username", auth.DefaultAdminUsername, "Target username to reset")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// CLI 运行初始化 logger 到 stderr（避免污染 stdout）
	if err := logger.Init(cfg); err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Sync()

	store, err := storage.New(cfg)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()

	user, err := store.GetUserByUsername(*username)
	if err != nil {
		return fmt.Errorf("user %q not found (use etcdmonitor init or start the server to create default admin)", *username)
	}

	minLen := cfg.Auth.MinPasswordLength
	if minLen <= 0 {
		minLen = 8
	}

	newPass, err := readPasswordTwice("New password: ", "Retype new password: ", minLen)
	if err != nil {
		return err
	}

	hash, err := auth.HashPassword(newPass, cfg.Auth.BcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// must_change=1：防止 CLI 执行者记住密码 —— 目标用户下次登录必须再改一次
	if err := store.UpdatePassword(*username, hash, true); err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	// 审计日志
	hostname, _ := os.Hostname()
	entry := storage.AuditEntry{
		Timestamp: time.Now().Unix(),
		Username:  *username,
		Operation: "cli_reset_password",
		Target:    "local@" + hostname,
		Result:    "ok",
		Success:   true,
	}
	_ = store.StoreAuditLog(entry)

	fmt.Printf("[Admin] Password reset for user=%s. User must change it on next login.\n", *username)
	_ = user // suppress unused warning when above logic simplifies
	return nil
}

// runUnlock 实现 `etcdmonitor unlock` 子命令：只清零 failed_attempts / locked_until
func runUnlock(args []string) error {
	fs := flag.NewFlagSet("unlock", flag.ContinueOnError)
	configPath := fs.String("config", "config.yaml", "Path to config file")
	username := fs.String("username", auth.DefaultAdminUsername, "Target username to unlock")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := logger.Init(cfg); err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Sync()

	store, err := storage.New(cfg)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()

	if _, err := store.GetUserByUsername(*username); err != nil {
		return fmt.Errorf("user %q not found", *username)
	}
	if err := store.ResetLoginState(*username); err != nil {
		return fmt.Errorf("unlock failed: %w", err)
	}

	hostname, _ := os.Hostname()
	entry := storage.AuditEntry{
		Timestamp: time.Now().Unix(),
		Username:  *username,
		Operation: "cli_unlock",
		Target:    "local@" + hostname,
		Result:    "ok",
		Success:   true,
	}
	_ = store.StoreAuditLog(entry)

	fmt.Printf("[Admin] Unlocked user=%s (failed_attempts=0, locked_until=0)\n", *username)
	return nil
}

// readPasswordTwice 读取两次密码并校验长度与一致性
func readPasswordTwice(prompt1, prompt2 string, minLen int) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("stdin is not a terminal; refuse to read password from pipe")
	}

	fmt.Print(prompt1)
	p1, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	pass1 := strings.TrimRight(string(p1), "\r\n")

	if len(pass1) < minLen {
		return "", fmt.Errorf("password too short (minimum %d characters)", minLen)
	}

	fmt.Print(prompt2)
	p2, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	pass2 := strings.TrimRight(string(p2), "\r\n")

	if pass1 != pass2 {
		return "", fmt.Errorf("passwords do not match")
	}
	return pass1, nil
}
