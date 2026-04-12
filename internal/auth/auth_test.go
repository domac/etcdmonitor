package auth

import (
	"sync"
	"testing"
	"time"
)

func TestGenerateToken(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("expected 64-char hex token, got %d chars", len(token))
	}
}

func TestGenerateTokenUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken failed: %v", err)
		}
		if seen[token] {
			t.Fatalf("duplicate token generated at iteration %d", i)
		}
		seen[token] = true
	}
}

func TestSessionIsExpired(t *testing.T) {
	s := &Session{ExpiresAt: time.Now().Add(-1 * time.Minute)}
	if !s.IsExpired() {
		t.Error("expected session to be expired")
	}

	s2 := &Session{ExpiresAt: time.Now().Add(1 * time.Hour)}
	if s2.IsExpired() {
		t.Error("expected session to not be expired")
	}
}

func TestMemorySessionStoreCreate(t *testing.T) {
	store := NewMemorySessionStore()
	defer store.Stop()

	session, err := store.Create("testuser", 1*time.Hour)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if session.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %q", session.Username)
	}
	if session.Token == "" {
		t.Error("expected non-empty token")
	}
	if session.IsExpired() {
		t.Error("newly created session should not be expired")
	}
}

func TestMemorySessionStoreGet(t *testing.T) {
	store := NewMemorySessionStore()
	defer store.Stop()

	session, _ := store.Create("testuser", 1*time.Hour)

	got := store.Get(session.Token)
	if got == nil {
		t.Fatal("expected to get session, got nil")
	}
	if got.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %q", got.Username)
	}

	// Non-existent token
	if store.Get("nonexistent") != nil {
		t.Error("expected nil for non-existent token")
	}
}

func TestMemorySessionStoreGetExpired(t *testing.T) {
	store := NewMemorySessionStore()
	defer store.Stop()

	session, _ := store.Create("testuser", 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	got := store.Get(session.Token)
	if got != nil {
		t.Error("expected nil for expired session")
	}
}

func TestMemorySessionStoreDelete(t *testing.T) {
	store := NewMemorySessionStore()
	defer store.Stop()

	session, _ := store.Create("testuser", 1*time.Hour)
	store.Delete(session.Token)

	if store.Get(session.Token) != nil {
		t.Error("expected nil after delete")
	}
}

func TestMemorySessionStoreIsValid(t *testing.T) {
	store := NewMemorySessionStore()
	defer store.Stop()

	session, _ := store.Create("testuser", 1*time.Hour)

	if !store.IsValid(session.Token) {
		t.Error("expected valid session")
	}
	if store.IsValid("nonexistent") {
		t.Error("expected invalid for non-existent token")
	}
}

func TestMemorySessionStoreConcurrency(t *testing.T) {
	store := NewMemorySessionStore()
	defer store.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := store.Create("user", 1*time.Hour)
			if err != nil {
				t.Errorf("concurrent Create failed: %v", err)
				return
			}
			store.IsValid(s.Token)
			store.Get(s.Token)
			store.Delete(s.Token)
		}()
	}
	wg.Wait()
}

func TestMemorySessionStoreCleanup(t *testing.T) {
	store := NewMemorySessionStore()
	defer store.Stop()

	// Create expired and valid sessions
	store.Create("expired", 1*time.Millisecond)
	valid, _ := store.Create("valid", 1*time.Hour)
	time.Sleep(5 * time.Millisecond)

	store.cleanup()

	if store.Get(valid.Token) == nil {
		t.Error("valid session should survive cleanup")
	}

	// The expired session should have been cleaned
	store.mu.RLock()
	count := len(store.sessions)
	store.mu.RUnlock()
	if count != 1 {
		t.Errorf("expected 1 session after cleanup, got %d", count)
	}
}
