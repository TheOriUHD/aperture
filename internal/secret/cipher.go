// Package secret provides authenticated at-rest encryption for small sensitive
// values — currently storage-location credentials (SMB/SFTP passwords and keys).
// It is deliberately separate from store so the persistence layer only ever
// handles opaque ciphertext and never imports crypto.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// ErrNoKey is returned by New when no key is configured (encryption unavailable).
var ErrNoKey = errors.New("secret: no encryption key configured")

// Cipher encrypts/decrypts short secrets with AES-256-GCM. The output of Encrypt
// is base64(nonce‖ciphertext‖tag); Decrypt authenticates before returning.
type Cipher struct {
	aead cipher.AEAD
}

// New builds a Cipher from a 16/24/32-byte key (32 = AES-256, as Aperture uses).
func New(key []byte) (*Cipher, error) {
	if len(key) == 0 {
		return nil, ErrNoKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt seals plaintext and returns a base64 string safe to store as text.
// Empty input yields empty output (a location simply has no credential).
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. Empty input yields empty output. A tampered or
// wrong-key ciphertext fails authentication and returns an error.
func (c *Cipher) Decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("secret: ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
