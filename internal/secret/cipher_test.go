package secret

import (
	"crypto/rand"
	"strings"
	"testing"
)

func key(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := New(key(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, pt := range []string{"", "hunter2", "a very long share password with spaces & symbols: /\\@#"} {
		enc, err := c.Encrypt(pt)
		if err != nil {
			t.Fatalf("encrypt %q: %v", pt, err)
		}
		if pt != "" && enc == pt {
			t.Fatalf("ciphertext equals plaintext for %q", pt)
		}
		got, err := c.Decrypt(enc)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if got != pt {
			t.Fatalf("round-trip mismatch: got %q want %q", got, pt)
		}
	}
}

func TestEncryptUsesFreshNonce(t *testing.T) {
	c, _ := New(key(t))
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Fatal("expected different ciphertexts for repeated plaintext (nonce reuse?)")
	}
}

func TestDecryptRejectsTampered(t *testing.T) {
	c, _ := New(key(t))
	enc, _ := c.Encrypt("secret")
	// Flip a character in the middle of the base64 blob.
	b := []byte(enc)
	mid := len(b) / 2
	if b[mid] == 'A' {
		b[mid] = 'B'
	} else {
		b[mid] = 'A'
	}
	if _, err := c.Decrypt(string(b)); err == nil {
		t.Fatal("expected authentication failure on tampered ciphertext")
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	c1, _ := New(key(t))
	c2, _ := New(key(t))
	enc, _ := c1.Encrypt("secret")
	if _, err := c2.Decrypt(enc); err == nil {
		t.Fatal("expected failure decrypting with a different key")
	}
}

func TestNewRejectsEmptyKey(t *testing.T) {
	if _, err := New(nil); err == nil || !strings.Contains(err.Error(), "no encryption key") {
		t.Fatalf("expected ErrNoKey, got %v", err)
	}
}
