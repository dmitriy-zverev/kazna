package credentials

import (
	"strings"
	"testing"
)

func TestGenerateShape(t *testing.T) {
	k, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(k.FullKey, KeyPrefix) {
		t.Fatalf("key %q does not start with %q", k.FullKey, KeyPrefix)
	}
	if len(k.FullKey) > 64 {
		t.Fatalf("key length %d exceeds the 64-char contract limit", len(k.FullKey))
	}
	if len(k.Prefix) != prefixLen {
		t.Fatalf("prefix length = %d, want %d", len(k.Prefix), prefixLen)
	}
	if len(k.Hash) != 32 {
		t.Fatalf("hash length = %d, want 32", len(k.Hash))
	}
	if !LooksValid(k.FullKey) {
		t.Fatal("generated key must pass LooksValid")
	}
}

func TestGenerateIsUnique(t *testing.T) {
	seenKeys := make(map[string]bool, 1000)
	seenPrefixes := make(map[string]bool, 1000)

	for i := 0; i < 1000; i++ {
		k, err := Generate()
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if seenKeys[k.FullKey] {
			t.Fatalf("duplicate key after %d iterations", i)
		}
		if seenPrefixes[k.Prefix] {
			t.Fatalf("duplicate prefix %q after %d iterations", k.Prefix, i)
		}
		seenKeys[k.FullKey] = true
		seenPrefixes[k.Prefix] = true
	}
}

func TestVerify(t *testing.T) {
	k, _ := Generate()

	if !Verify(k.Hash, k.FullKey) {
		t.Fatal("valid key rejected")
	}
	if Verify(k.Hash, k.FullKey+"x") {
		t.Fatal("tampered key accepted")
	}
	if Verify(k.Hash, strings.ToUpper(k.FullKey)) {
		t.Fatal("case-changed key accepted")
	}
	if Verify(nil, k.FullKey) {
		t.Fatal("empty hash accepted")
	}
}

func TestPrefixOfIsStableAndShort(t *testing.T) {
	k, _ := Generate()
	if got := PrefixOf(k.FullKey); got != k.Prefix {
		t.Fatalf("PrefixOf = %q, want %q", got, k.Prefix)
	}
	if PrefixOf("short") != "short" {
		t.Fatal("short input must be returned unchanged")
	}
}

func TestLooksValidRejectsJunk(t *testing.T) {
	for _, bad := range []string{"", "app_live_", "app_test_abc", "bearer x", "app_live_!!!", "sk_live_abc"} {
		if LooksValid(bad) {
			t.Fatalf("LooksValid(%q) = true, want false", bad)
		}
	}
}
