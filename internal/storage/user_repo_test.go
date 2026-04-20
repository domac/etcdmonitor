package storage

import (
	"testing"
	"time"
)

func TestUserRepo_CreateAndGet(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")

	u := &User{
		Username:           "admin",
		PasswordHash:       "$2a$10$fakehash",
		Role:               "admin",
		MustChangePassword: true,
	}
	if err := s.CreateUser(u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == 0 {
		t.Fatalf("CreateUser should set ID, got 0")
	}

	got, err := s.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.Username != "admin" || got.PasswordHash != "$2a$10$fakehash" || !got.MustChangePassword {
		t.Fatalf("GetUserByUsername mismatch: %+v", got)
	}
	if got.Role != "admin" {
		t.Fatalf("Role mismatch: %s", got.Role)
	}
}

func TestUserRepo_GetNotFound(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")
	if _, err := s.GetUserByUsername("ghost"); err != ErrUserNotFound {
		t.Fatalf("want ErrUserNotFound, got %v", err)
	}
}

func TestUserRepo_CountUsers(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")
	n, err := s.CountUsers()
	if err != nil || n != 0 {
		t.Fatalf("empty count = %d, err=%v", n, err)
	}
	_ = s.CreateUser(&User{Username: "a", PasswordHash: "h", Role: "admin"})
	_ = s.CreateUser(&User{Username: "b", PasswordHash: "h", Role: "admin"})
	n, _ = s.CountUsers()
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
}

func TestUserRepo_UpdatePassword(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")
	_ = s.CreateUser(&User{
		Username: "admin", PasswordHash: "old", Role: "admin", MustChangePassword: true,
	})
	// 先触发一次 failed 累加
	if _, err := s.IncrementFailedAttempts("admin"); err != nil {
		t.Fatalf("inc: %v", err)
	}
	if err := s.UpdatePassword("admin", "new_hash", false); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	u, _ := s.GetUserByUsername("admin")
	if u.PasswordHash != "new_hash" {
		t.Fatalf("hash not updated")
	}
	if u.MustChangePassword {
		t.Fatalf("must_change should be 0")
	}
	if u.FailedAttempts != 0 || u.LockedUntil != 0 {
		t.Fatalf("UpdatePassword must reset failed/locked, got %d/%d", u.FailedAttempts, u.LockedUntil)
	}
}

func TestUserRepo_UpdatePasswordNotFound(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")
	if err := s.UpdatePassword("ghost", "h", true); err != ErrUserNotFound {
		t.Fatalf("want ErrUserNotFound, got %v", err)
	}
}

func TestUserRepo_LockoutFlow(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")
	_ = s.CreateUser(&User{Username: "u", PasswordHash: "h", Role: "admin"})

	for i := 1; i <= 3; i++ {
		n, err := s.IncrementFailedAttempts("u")
		if err != nil {
			t.Fatalf("inc %d: %v", i, err)
		}
		if n != i {
			t.Fatalf("inc count = %d, want %d", n, i)
		}
	}

	until := time.Now().Unix() + 900
	if err := s.SetLocked("u", until); err != nil {
		t.Fatalf("SetLocked: %v", err)
	}
	u, _ := s.GetUserByUsername("u")
	if !u.IsLocked(time.Now()) {
		t.Fatalf("should be locked")
	}
	if u.FailedAttempts != 3 {
		t.Fatalf("failed attempts lost: %d", u.FailedAttempts)
	}

	if err := s.ResetLoginState("u"); err != nil {
		t.Fatalf("ResetLoginState: %v", err)
	}
	u, _ = s.GetUserByUsername("u")
	if u.IsLocked(time.Now()) || u.FailedAttempts != 0 {
		t.Fatalf("should be unlocked & reset")
	}
}

func TestUserRepo_UpdateLastLogin(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")
	_ = s.CreateUser(&User{Username: "u", PasswordHash: "h", Role: "admin"})
	before, _ := s.GetUserByUsername("u")
	time.Sleep(10 * time.Millisecond)
	if err := s.UpdateLastLogin("u"); err != nil {
		t.Fatalf("UpdateLastLogin: %v", err)
	}
	after, _ := s.GetUserByUsername("u")
	if after.LastLoginAt < before.LastLoginAt {
		t.Fatalf("last_login_at not advanced")
	}
	if after.LastLoginAt == 0 {
		t.Fatalf("last_login_at zero")
	}
}

func TestUserRepo_UniqueUsername(t *testing.T) {
	s := newTestStorage(t, "http://127.0.0.1:2379")
	if err := s.CreateUser(&User{Username: "dup", PasswordHash: "h", Role: "admin"}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := s.CreateUser(&User{Username: "dup", PasswordHash: "h2", Role: "admin"}); err == nil {
		t.Fatalf("duplicate username should fail")
	}
}
