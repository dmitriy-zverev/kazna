package security

import (
	"bytes"
	"errors"
	"testing"
)

func mustKey(t *testing.T) string {
	t.Helper()
	k, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return k
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := NewCipher(mustKey(t), "")
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}

	for _, plaintext := range []string{"", "secret_key_123", string(make([]byte, 4096))} {
		ct, err := c.Encrypt([]byte(plaintext))
		if err != nil {
			t.Fatalf("Encrypt(%d bytes): %v", len(plaintext), err)
		}
		if bytes.Contains(ct, []byte("secret_key_123")) && plaintext == "secret_key_123" {
			t.Fatal("plaintext leaked into ciphertext")
		}
		got, err := c.Decrypt(ct)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if !bytes.Equal(got, []byte(plaintext)) {
			t.Fatalf("round trip mismatch: got %q want %q", got, plaintext)
		}
	}
}

func TestNonceIsRandomPerCall(t *testing.T) {
	c, _ := NewCipher(mustKey(t), "")
	a, _ := c.Encrypt([]byte("same"))
	b, _ := c.Encrypt([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("identical ciphertext for identical plaintext: nonce is not random")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	c, _ := NewCipher(mustKey(t), "")
	ct, _ := c.Encrypt([]byte("secret"))

	ct[len(ct)-1] ^= 0xFF
	if _, err := c.Decrypt(ct); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("tampered ciphertext: err = %v, want ErrDecrypt", err)
	}
}

func TestDecryptRejectsTruncatedInput(t *testing.T) {
	c, _ := NewCipher(mustKey(t), "")
	if _, err := c.Decrypt([]byte{1, 2}); !errors.Is(err, ErrCiphertextShort) {
		t.Fatalf("err = %v, want ErrCiphertextShort", err)
	}
}

func TestDecryptRejectsUnknownKey(t *testing.T) {
	old, _ := NewCipher(mustKey(t), "")
	ct, _ := old.Encrypt([]byte("secret"))

	rotated, _ := NewCipher(mustKey(t), "")
	if _, err := rotated.Decrypt(ct); !errors.Is(err, ErrUnknownKeyID) {
		t.Fatalf("err = %v, want ErrUnknownKeyID", err)
	}
}

func TestRotationKeepsOldCiphertextReadable(t *testing.T) {
	previous := mustKey(t)
	oldCipher, _ := NewCipher(previous, "")
	ct, _ := oldCipher.Encrypt([]byte("provider-secret"))

	// New primary, old key retained for decryption.
	newPrimary := mustKey(t)
	rotated, err := NewCipher(newPrimary, previous)
	if err != nil {
		t.Fatalf("NewCipher with previous: %v", err)
	}

	got, err := rotated.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt after rotation: %v", err)
	}
	if string(got) != "provider-secret" {
		t.Fatalf("got %q, want provider-secret", got)
	}

	// New writes must use the new primary, so dropping the old key later is safe.
	fresh, _ := rotated.Encrypt([]byte("new"))
	onlyNew, _ := NewCipher(newPrimary, "")
	if _, err := onlyNew.Decrypt(fresh); err != nil {
		t.Fatalf("fresh ciphertext must be readable by the new primary alone: %v", err)
	}
	// The old key alone must not read new writes.
	onlyOld, _ := NewCipher(previous, "")
	if _, err := onlyOld.Decrypt(fresh); !errors.Is(err, ErrUnknownKeyID) {
		t.Fatalf("old key read new ciphertext: err = %v, want ErrUnknownKeyID", err)
	}
}

func TestParseKeyRejectsBadInput(t *testing.T) {
	for _, bad := range []string{"", "nothex", "abcd", "0f0f0f"} {
		if _, err := ParseKey(bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("ParseKey(%q): err = %v, want ErrInvalidKey", bad, err)
		}
	}
}
