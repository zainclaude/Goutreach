// Package crypto provides authenticated symmetric encryption (AES-256-GCM) for
// secrets stored at rest, such as SMTP/IMAP passwords.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Cipher encrypts and decrypts short secrets using AES-256-GCM.
type Cipher struct {
	aead cipher.AEAD
}

// New builds a Cipher from a key. The key must be at least 32 bytes; the first
// 32 bytes are used (AES-256).
func New(key string) (*Cipher, error) {
	if len(key) < 32 {
		return nil, fmt.Errorf("key must be >= 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher([]byte(key)[:32])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt returns a base64-encoded ciphertext (nonce prepended).
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt reverses Encrypt.
func (c *Cipher) Decrypt(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
