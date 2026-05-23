// Package tabs 提供 KV Tree 多集群 Tab 管理能力。
//
// 该包负责：
//   - 用户新增的远程 etcd 集群 Tab 的 CRUD（按 user_id 隔离）
//   - 凭据 AES-256-GCM 加密落盘
//   - 后台定时探活
//   - 按 Tab 维度路由 KV 请求（与 cmd/etcdmonitor/main.go + handler.go 配合）
package tabs

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"etcdmonitor/internal/logger"
)

// kekFileName KEK 文件名（位于 data/ 目录下，0600 权限）。
const kekFileName = "kv_tabs.key"

// kekSize KEK 字节长度（AES-256 要求 32 字节）。
const kekSize = 32

// nonceSize AES-GCM 标准 nonce 长度。
const nonceSize = 12

// 加解密相关错误。
var (
	// ErrKEKMissing 表示 KEK 文件丢失或权限不足无法读取。
	ErrKEKMissing = errors.New("KV_TAB_KEK_MISSING")
	// ErrCipherInvalid 表示密文格式不合法或解密失败。
	ErrCipherInvalid = errors.New("KV_TAB_CIPHER_INVALID")
)

// KeyManager 抽象凭据加解密；预留 Rotate() 给未来 KEK 轮换扩展。
type KeyManager interface {
	// Encrypt 用当前 KEK 加密 plaintext，返回 [12B nonce | ciphertext | 16B tag]。
	// plaintext 为空时返回空切片，方便"无密码"场景。
	Encrypt(plaintext []byte) ([]byte, error)

	// Decrypt 解密由 Encrypt 产出的密文；空切片直接返回空切片。
	Decrypt(cipherText []byte) ([]byte, error)

	// Rotate 预留接口；本变更不实现 KEK 轮换，调用必须返回 ErrNotImplemented。
	Rotate() error
}

// ErrNotImplemented 表示该接口尚未实现。
var ErrNotImplemented = errors.New("not implemented")

// FileKeyManager 是从 data/kv_tabs.key 读取 KEK 的实现。
//
// 文件不存在时自动生成 32 字节随机 KEK 并以 0600 写入；
// 权限超出 0600 时尝试 chmod 收紧（失败仅 WARN，不阻塞业务）。
type FileKeyManager struct {
	path string

	mu  sync.RWMutex
	kek []byte // 32 字节
	gcm cipher.AEAD
}

// NewFileKeyManager 在指定 data 目录初始化 FileKeyManager。
//
// 若 dataDir/kv_tabs.key 不存在，自动生成；存在但权限超出 0600，自动 chmod。
// 加载失败返回 error；返回的实例可被多 goroutine 安全使用。
func NewFileKeyManager(dataDir string) (*FileKeyManager, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("dataDir is required")
	}
	path := filepath.Join(dataDir, kekFileName)

	m := &FileKeyManager{path: path}
	if err := m.loadOrCreate(); err != nil {
		return nil, err
	}
	return m, nil
}

// loadOrCreate 装载或新建 KEK 文件，并初始化 AES-GCM cipher。
func (m *FileKeyManager) loadOrCreate() error {
	// 先尝试读取
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			// 不存在 → 生成
			return m.generateAndSave()
		}
		return fmt.Errorf("read KEK file: %w", err)
	}

	// 校验长度
	if len(data) != kekSize {
		return fmt.Errorf("KEK file %s has invalid length %d (expected %d): %w",
			m.path, len(data), kekSize, ErrKEKMissing)
	}

	// 检查权限并尝试收紧
	m.tightenPermsBestEffort()

	return m.installKey(data)
}

// generateAndSave 用 crypto/rand 生成 32 字节 KEK 并以 0600 写入。
func (m *FileKeyManager) generateAndSave() error {
	kek := make([]byte, kekSize)
	if _, err := io.ReadFull(rand.Reader, kek); err != nil {
		return fmt.Errorf("generate KEK: %w", err)
	}

	// 确保目录存在且权限收紧
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("ensure data dir: %w", err)
	}

	// 0600 写入；OpenFile + WriteFile 二选一——这里用 OpenFile 显式控制权限，
	// 避免在某些平台 / umask 下 0600 退化为 0644。
	f, err := os.OpenFile(m.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create KEK file: %w", err)
	}
	if _, err := f.Write(kek); err != nil {
		_ = f.Close()
		return fmt.Errorf("write KEK file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close KEK file: %w", err)
	}

	logger.Infof("[KV-Tabs] Generated new KEK at %s (0600). 注意：此文件保护远程 Tab 凭据，"+
		"请妥善备份；丢失后所有 Tab 密码必须重新输入", m.path)

	return m.installKey(kek)
}

// installKey 装入 KEK 并构造 AES-GCM cipher。
func (m *FileKeyManager) installKey(kek []byte) error {
	block, err := aes.NewCipher(kek)
	if err != nil {
		return fmt.Errorf("init AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("init GCM: %w", err)
	}

	m.mu.Lock()
	m.kek = make([]byte, len(kek))
	copy(m.kek, kek)
	m.gcm = gcm
	m.mu.Unlock()
	return nil
}

// tightenPermsBestEffort 检查并尝试把 KEK 文件权限收紧到 0600。
// 失败仅 WARN，不阻塞业务（不同文件系统可能不支持 chmod）。
func (m *FileKeyManager) tightenPermsBestEffort() {
	info, err := os.Stat(m.path)
	if err != nil {
		return
	}
	// Unix 权限位
	mode := info.Mode().Perm()
	if mode&0o077 != 0 {
		// 有 group / other 权限，超出 0600
		logger.Warnf("[KV-Tabs] KEK file %s has loose permissions %o, attempting to chmod 0600",
			m.path, mode)
		if err := os.Chmod(m.path, 0o600); err != nil {
			logger.Warnf("[KV-Tabs] chmod 0600 failed for %s: %v (each subsequent access will warn)",
				m.path, err)
		}
	}
}

// Encrypt 实现 KeyManager.Encrypt。
//
// 输出格式：[nonce(12B) | ciphertext | tag(16B)]，长度 = len(plaintext) + 28
// plaintext 长度为 0 时直接返回空切片，方便"无密码"场景。
func (m *FileKeyManager) Encrypt(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return []byte{}, nil
	}
	m.mu.RLock()
	gcm := m.gcm
	m.mu.RUnlock()
	if gcm == nil {
		return nil, ErrKEKMissing
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	// gcm.Seal 输出 = ciphertext || tag；前缀 nonce 让密文自包含。
	out := gcm.Seal(nonce, nonce, plaintext, nil)
	return out, nil
}

// Decrypt 实现 KeyManager.Decrypt。
//
// 输入格式必须是 [nonce(12B) | ciphertext | tag(16B)]；
// 空切片直接返回空切片（兼容"未设置密码"的 Tab）。
func (m *FileKeyManager) Decrypt(cipherText []byte) ([]byte, error) {
	if len(cipherText) == 0 {
		return []byte{}, nil
	}
	m.mu.RLock()
	gcm := m.gcm
	m.mu.RUnlock()
	if gcm == nil {
		return nil, ErrKEKMissing
	}

	if len(cipherText) < nonceSize+gcm.Overhead() {
		return nil, fmt.Errorf("cipher too short: %w", ErrCipherInvalid)
	}
	nonce, payload := cipherText[:nonceSize], cipherText[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return nil, fmt.Errorf("gcm open: %w: %w", err, ErrCipherInvalid)
	}
	return plaintext, nil
}

// Rotate 预留接口；本变更不实现。
func (m *FileKeyManager) Rotate() error {
	return ErrNotImplemented
}
