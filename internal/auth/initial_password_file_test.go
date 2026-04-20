package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInitialPasswordFile_WriteReadDelete(t *testing.T) {
	dir := t.TempDir()
	const pwd = "Qw3xLmN9pK7vR2Bf"

	if err := WriteInitialPasswordFile(dir, pwd); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !InitialPasswordFileExists(dir) {
		t.Fatalf("Exists should be true after write")
	}

	data, err := os.ReadFile(InitialPasswordFilePath(dir))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != pwd+"\n" {
		t.Fatalf("file content = %q, want %q", string(data), pwd+"\n")
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(InitialPasswordFilePath(dir))
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
		}
	}

	if err := DeleteInitialPasswordFile(dir); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if InitialPasswordFileExists(dir) {
		t.Fatalf("Exists should be false after delete")
	}

	// 再次删除应视为成功（幂等）
	if err := DeleteInitialPasswordFile(dir); err != nil {
		t.Fatalf("Delete idempotent: %v", err)
	}
}

func TestInitialPasswordFile_OverwriteIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := WriteInitialPasswordFile(dir, "first"); err != nil {
		t.Fatalf("write1: %v", err)
	}
	if err := WriteInitialPasswordFile(dir, "second"); err != nil {
		t.Fatalf("write2: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, InitialPasswordFileName))
	if string(data) != "second\n" {
		t.Fatalf("overwrite failed: %q", string(data))
	}
}
