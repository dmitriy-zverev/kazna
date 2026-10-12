// Package credentials issues and verifies application API keys.
//
// A key is shown to its owner exactly once. Only a SHA-256 hash plus a public
// lookup prefix are stored (docs/SECURITY.md §2).
package credentials

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	// KeyPrefix marks a live application key, e.g. app_live_xxxxx.
	KeyPrefix = "app_live_"

	tokenBytes = 32
	// prefixLen is how many leading characters are stored for lookup. It must
	// stay well below the full key length so it leaks nothing useful.
	prefixLen = 16
)

// ErrMalformedKey is returned for input that cannot be an application key.
var ErrMalformedKey = errors.New("credentials: malformed api key")

// Key is a freshly generated credential. FullKey is returned to the caller
// once and never persisted.
type Key struct {
	FullKey string
	Prefix  string
	Hash    []byte
}

// Generate mints a new API key with a cryptographically secure random token.
func Generate() (Key, error) {
	token := make([]byte, tokenBytes)
	if _, err := io.ReadFull(rand.Reader, token); err != nil {
		return Key{}, fmt.Errorf("credentials: random token: %w", err)
	}

	full := KeyPrefix + base64.RawURLEncoding.EncodeToString(token)
	return Key{
		FullKey: full,
		Prefix:  PrefixOf(full),
		Hash:    Hash(full),
	}, nil
}

// Hash returns the stored representation of a key.
func Hash(fullKey string) []byte {
	sum := sha256.Sum256([]byte(fullKey))
	return sum[:]
}

// PrefixOf returns the public lookup prefix of a key.
func PrefixOf(fullKey string) string {
	if len(fullKey) < prefixLen {
		return fullKey
	}
	return fullKey[:prefixLen]
}

// LooksValid reports whether s has the shape of an application key, so callers
// can reject junk before touching the database.
func LooksValid(s string) bool {
	if !strings.HasPrefix(s, KeyPrefix) {
		return false
	}
	token := strings.TrimPrefix(s, KeyPrefix)
	if token == "" {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil
}

// Verify compares a presented key against a stored hash in constant time.
func Verify(storedHash []byte, presented string) bool {
	if len(storedHash) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare(storedHash, Hash(presented)) == 1
}
