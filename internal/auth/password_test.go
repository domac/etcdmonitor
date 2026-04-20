package auth

import (
	"testing"
)

func TestHashPassword_AndCompare(t *testing.T) {
	hash, err := HashPassword("Hello-World-12345", BcryptDefaultCost)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if len(hash) == 0 {
		t.Fatalf("empty hash")
	}
	if !ComparePassword(hash, "Hello-World-12345") {
		t.Fatalf("compare should match")
	}
	if ComparePassword(hash, "wrong") {
		t.Fatalf("compare should NOT match wrong plain")
	}
}

func TestResolveBcryptCost_Bounds(t *testing.T) {
	tests := []struct {
		in, want int
	}{
		{10, 10},
		{8, 8},
		{14, 14},
		{0, BcryptDefaultCost},  // below min → default
		{7, BcryptDefaultCost},  // below min → default
		{15, BcryptDefaultCost}, // above max → default
		{50, BcryptDefaultCost},
	}
	for _, c := range tests {
		if got := ResolveBcryptCost(c.in); got != c.want {
			t.Errorf("ResolveBcryptCost(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestGenerateInitialPassword_LengthAndCharset(t *testing.T) {
	p, err := GenerateInitialPassword()
	if err != nil {
		t.Fatalf("GenerateInitialPassword: %v", err)
	}
	if len(p) != InitialPasswordLength {
		t.Fatalf("length = %d, want %d", len(p), InitialPasswordLength)
	}
	forbidden := "0Oo1lI"
	for _, ch := range p {
		for _, f := range forbidden {
			if ch == f {
				t.Fatalf("password %q contains forbidden char %c", p, ch)
			}
		}
	}
	// 必须全部来自白名单
	alphabet := string(initialPasswordAlphabet)
	for _, ch := range p {
		if !containsRune(alphabet, ch) {
			t.Fatalf("char %c not in alphabet", ch)
		}
	}
}

func TestGenerateInitialPassword_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		p, err := GenerateInitialPassword()
		if err != nil {
			t.Fatalf("gen: %v", err)
		}
		if seen[p] {
			t.Fatalf("collision in 50 generations: %s", p)
		}
		seen[p] = true
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
