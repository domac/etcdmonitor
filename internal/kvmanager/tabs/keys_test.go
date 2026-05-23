package tabs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestNewFileKeyManager_GeneratesKEKWhenMissing 文件不存在时应自动生成 0600 文件。
func TestNewFileKeyManager_GeneratesKEKWhenMissing(t *testing.T) {
	dir := t.TempDir()
	km, err := NewFileKeyManager(dir)
	if err != nil {
		t.Fatalf("NewFileKeyManager: %v", err)
	}
	if km == nil {
		t.Fatal("got nil km")
	}

	path := filepath.Join(dir, kekFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat KEK file: %v", err)
	}
	if info.Size() != int64(kekSize) {
		t.Errorf("KEK file size = %d; want %d", info.Size(), kekSize)
	}

	// 权限校验仅在 Unix 上做（Windows 权限语义不同）
	if runtime.GOOS != "windows" {
		mode := info.Mode().Perm()
		if mode != 0o600 {
			t.Errorf("KEK file mode = %o; want 0600", mode)
		}
	}
}

// TestNewFileKeyManager_ReusesExistingKEK 已存在的合法 KEK 文件必须直接复用，不可被覆盖。
func TestNewFileKeyManager_ReusesExistingKEK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, kekFileName)

	want := bytes.Repeat([]byte{0xAB}, kekSize)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	km, err := NewFileKeyManager(dir)
	if err != nil {
		t.Fatalf("NewFileKeyManager: %v", err)
	}
	if !bytes.Equal(km.kek, want) {
		t.Errorf("KEK was overwritten; got %x", km.kek[:8])
	}
}

// TestNewFileKeyManager_InvalidLength 长度不对的 KEK 文件应返回 ErrKEKMissing。
func TestNewFileKeyManager_InvalidLength(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, kekFileName)
	if err := os.WriteFile(path, []byte("too short"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := NewFileKeyManager(dir)
	if err == nil {
		t.Fatal("expected error for invalid KEK length, got nil")
	}
	if !errors.Is(err, ErrKEKMissing) {
		t.Errorf("expected wrapped ErrKEKMissing, got %v", err)
	}
}

// TestNewFileKeyManager_TightensLoosePermissions 启动时遇到 0644，应自动 chmod 为 0600。
func TestNewFileKeyManager_TightensLoosePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission tightening is Unix-only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, kekFileName)
	want := bytes.Repeat([]byte{0xCD}, kekSize)
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := NewFileKeyManager(dir); err != nil {
		t.Fatalf("NewFileKeyManager: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("permissions not tightened: mode = %o, want 0600", mode)
	}
}

// TestEncryptDecryptRoundtrip 加密 → 解密应得回原文。
func TestEncryptDecryptRoundtrip(t *testing.T) {
	dir := t.TempDir()
	km, err := NewFileKeyManager(dir)
	if err != nil {
		t.Fatalf("NewFileKeyManager: %v", err)
	}

	cases := []string{
		"",                             // 空
		"x",                            // 单字符
		"hunter2",                      // 普通密码
		"中文密码 + emoji 🔐",            // 多字节
		string(bytes.Repeat([]byte{1}, 1024)), // 1KB
	}
	for _, plain := range cases {
		ct, err := km.Encrypt([]byte(plain))
		if err != nil {
			t.Errorf("Encrypt(%q): %v", plain, err)
			continue
		}
		if plain == "" {
			if len(ct) != 0 {
				t.Errorf("Encrypt(\"\") = %x; want empty", ct)
			}
			continue
		}
		// 长度 = nonce(12) + plaintext + tag(16) = plaintext + 28
		if len(ct) != len(plain)+nonceSize+16 {
			t.Errorf("Encrypt length = %d; want %d", len(ct), len(plain)+nonceSize+16)
		}
		got, err := km.Decrypt(ct)
		if err != nil {
			t.Errorf("Decrypt(%q): %v", plain, err)
			continue
		}
		if string(got) != plain {
			t.Errorf("Decrypt mismatch: got %q want %q", got, plain)
		}
	}
}

// TestEncryptProducesUniqueCiphertexts 相同 plaintext 多次加密产物必须不同（nonce 随机）。
func TestEncryptProducesUniqueCiphertexts(t *testing.T) {
	dir := t.TempDir()
	km, err := NewFileKeyManager(dir)
	if err != nil {
		t.Fatalf("NewFileKeyManager: %v", err)
	}

	plain := []byte("same-input")
	a, _ := km.Encrypt(plain)
	b, _ := km.Encrypt(plain)
	if bytes.Equal(a, b) {
		t.Error("two Encrypt calls produced identical output; nonce reuse?")
	}
}

// TestDecryptInvalidCiphertext 损坏 / 篡改的密文必须返回 ErrCipherInvalid。
func TestDecryptInvalidCiphertext(t *testing.T) {
	dir := t.TempDir()
	km, err := NewFileKeyManager(dir)
	if err != nil {
		t.Fatalf("NewFileKeyManager: %v", err)
	}

	// case 1: 太短
	if _, err := km.Decrypt([]byte{1, 2, 3}); err == nil {
		t.Error("expected error for too-short cipher")
	} else if !errors.Is(err, ErrCipherInvalid) {
		t.Errorf("expected ErrCipherInvalid, got %v", err)
	}

	// case 2: 长度合法但内容随机（tag 不匹配）
	bogus := bytes.Repeat([]byte{0xFF}, nonceSize+32)
	if _, err := km.Decrypt(bogus); err == nil {
		t.Error("expected error for tampered cipher")
	} else if !errors.Is(err, ErrCipherInvalid) {
		t.Errorf("expected ErrCipherInvalid, got %v", err)
	}

	// case 3: 篡改 nonce
	plain := []byte("secret")
	ct, _ := km.Encrypt(plain)
	tampered := append([]byte{}, ct...)
	tampered[0] ^= 0xFF
	if _, err := km.Decrypt(tampered); err == nil {
		t.Error("expected error for nonce-tampered cipher")
	}
}

// TestRotate_NotImplemented 现阶段必须返回 ErrNotImplemented。
func TestRotate_NotImplemented(t *testing.T) {
	dir := t.TempDir()
	km, err := NewFileKeyManager(dir)
	if err != nil {
		t.Fatalf("NewFileKeyManager: %v", err)
	}
	if err := km.Rotate(); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Rotate() = %v; want ErrNotImplemented", err)
	}
}

// TestNewFileKeyManager_RequiresDataDir 空 dataDir 必须报错。
func TestNewFileKeyManager_RequiresDataDir(t *testing.T) {
	if _, err := NewFileKeyManager(""); err == nil {
		t.Error("expected error for empty dataDir")
	}
}
