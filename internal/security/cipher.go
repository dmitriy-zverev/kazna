// Package security holds cryptographic primitives shared by the service.
//
// Provider secrets are encrypted with AES-256-GCM (docs/SECURITY.md §1). The
// master key never touches PostgreSQL; a database compromise alone must not
// reveal provider credentials.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Errors returned by the cipher. Callers treat all of them as "cannot decrypt".
var (
	ErrInvalidKey      = errors.New("security: master key must be 32 bytes")
	ErrCiphertextShort = errors.New("security: ciphertext is truncated")
	ErrUnsupportedVer  = errors.New("security: unsupported envelope version")
	ErrUnknownKeyID    = errors.New("security: no key matches ciphertext key id")
	ErrDecrypt         = errors.New("security: decryption failed")
)

const (
	envelopeVersion = 1
	keyIDLen        = 4
	nonceLen        = 12
	// version(1) + keyID(4) + nonce(12)
	headerLen = 1 + keyIDLen + nonceLen
)

// Cipher encrypts with the primary key and decrypts with the primary or any
// previous key, which is what makes master key rotation possible.
type Cipher struct {
	primaryID uint32
	primary   cipher.AEAD
	keys      map[uint32]cipher.AEAD
}

// NewCipher builds a cipher. primaryKey is required; previousKeys is an
// optional comma-separated list of retired keys, accepted for decryption only.
func NewCipher(primaryKey string, previousKeys string) (*Cipher, error) {
	raw, err := ParseKey(primaryKey)
	if err != nil {
		return nil, fmt.Errorf("security: primary key: %w", err)
	}
	primary, err := newAEAD(raw)
	if err != nil {
		return nil, err
	}

	c := &Cipher{
		primaryID: keyID(raw),
		primary:   primary,
		keys:      map[uint32]cipher.AEAD{keyID(raw): primary},
	}

	for _, part := range strings.Split(previousKeys, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		prevRaw, err := ParseKey(part)
		if err != nil {
			return nil, fmt.Errorf("security: previous key: %w", err)
		}
		aead, err := newAEAD(prevRaw)
		if err != nil {
			return nil, err
		}
		c.keys[keyID(prevRaw)] = aead
	}

	return c, nil
}

// Encrypt seals plaintext with the primary key. The nonce is random per call.
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("security: nonce: %w", err)
	}

	out := make([]byte, 0, headerLen+len(plaintext)+c.primary.Overhead())
	out = append(out, envelopeVersion)
	out = append(out, idBytes(c.primaryID)...)
	out = append(out, nonce...)
	return c.primary.Seal(out, nonce, plaintext, nil), nil
}

// Decrypt opens an envelope produced by Encrypt.
func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < headerLen {
		return nil, ErrCiphertextShort
	}
	if ciphertext[0] != envelopeVersion {
		return nil, ErrUnsupportedVer
	}

	kid := uint32(ciphertext[1])<<24 | uint32(ciphertext[2])<<16 | uint32(ciphertext[3])<<8 | uint32(ciphertext[4])
	aead, ok := c.keys[kid]
	if !ok {
		return nil, ErrUnknownKeyID
	}

	nonce := ciphertext[5:headerLen]
	plaintext, err := aead.Open(nil, nonce, ciphertext[headerLen:], nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("security: aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("security: gcm: %w", err)
	}
	return aead, nil
}

// keyID derives a stable 4-byte identifier from the key material, so the id
// survives reordering of the key list during rotation.
func keyID(key []byte) uint32 {
	sum := sha256.Sum256(key)
	return uint32(sum[0])<<24 | uint32(sum[1])<<16 | uint32(sum[2])<<8 | uint32(sum[3])
}

func idBytes(id uint32) []byte {
	return []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
}

// ParseKey accepts a 32-byte key as hex or base64 (standard or URL, padded or raw).
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, ErrInvalidKey
	}

	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, ErrInvalidKey
}

// GenerateKey returns a fresh 32-byte key as hex, for provisioning and tests.
func GenerateKey() (string, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("security: generate key: %w", err)
	}
	return hex.EncodeToString(key), nil
}
