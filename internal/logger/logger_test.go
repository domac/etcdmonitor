package logger

import (
	"testing"

	"etcdmonitor/internal/config"
)

func TestNilSafety(t *testing.T) {
	// 确保 logger 未初始化状态
	sugar = nil
	zapLogger = nil

	// 所有全局函数调用不应 panic
	Debug("test")
	Debugf("test %s", "arg")
	Info("test")
	Infof("test %s", "arg")
	Warn("test")
	Warnf("test %s", "arg")
	Error("test")
	Errorf("test %s", "arg")
	// 注意：不测试 Fatal/Fatalf，因为它们会调用 os.Exit
}

func TestL_ReturnsNopLogger(t *testing.T) {
	sugar = nil
	zapLogger = nil

	l := L()
	if l == nil {
		t.Fatal("L() returned nil, expected nop logger")
	}

	// nop logger 应该能正常调用而不 panic
	l.Info("test nop logger")
}

func TestIsInitialized_False(t *testing.T) {
	sugar = nil

	if IsInitialized() {
		t.Error("IsInitialized() = true, want false when sugar is nil")
	}
}

func TestInit_AndIsInitialized(t *testing.T) {
	dir := t.TempDir()

	cfg := &config.Config{}
	cfg.Log.Dir = dir
	cfg.Log.Filename = "test.log"
	cfg.Log.Level = "info"
	cfg.Log.MaxSizeMB = 10
	cfg.Log.MaxFiles = 3
	cfg.Log.MaxAge = 7
	cfg.Log.Console = false

	if err := Init(cfg); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer func() {
		Sync()
		// 恢复为 nil，避免影响其他测试
		sugar = nil
		zapLogger = nil
	}()

	if !IsInitialized() {
		t.Error("IsInitialized() = false after Init()")
	}

	// 初始化后 L() 应返回真实 logger（非 nop）
	l := L()
	if l == nil {
		t.Fatal("L() returned nil after Init()")
	}

	// 全局函数应能正常工作
	Info("test after init")
	Infof("test %s after init", "formatted")
}

func TestSync_NilSafe(t *testing.T) {
	sugar = nil
	// Sync() 不应 panic
	Sync()
}
