// Package crypto provides symmetric encryption for stored secrets such as
// Claude tokens, and a helper for deriving a safe-to-display suffix from a
// token so full values are never logged or returned.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

// Box seals and opens byte slices with AES-256-GCM.
type Box struct{ aead cipher.AEAD }

// NewBox derives a 32-byte key from secret with SHA-256 and returns an
// AES-256-GCM box. It errors when secret is shorter than 32 bytes, since a
// short secret indicates a misconfigured or accidentally weak encryption key.
func NewBox(secret string) (*Box, error) {
	if len(secret) < 32 {
		return nil, errors.New("crypto: secret must be at least 32 bytes")
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plain with a fresh random 12-byte nonce and returns the
// ciphertext and nonce separately.
func (b *Box) Seal(plain []byte) (ciphertext, nonce []byte, err error) {
	nonce = make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ciphertext = b.aead.Seal(nil, nonce, plain, nil)
	return ciphertext, nonce, nil
}

// Open decrypts ciphertext using nonce, returning an error when the key is
// wrong or the ciphertext has been tampered with.
func (b *Box) Open(ciphertext, nonce []byte) ([]byte, error) {
	return b.aead.Open(nil, nonce, ciphertext, nil)
}

// Suffix returns the last 6 characters of a token for display (e.g.
// "sk-ant-oat01-abcdef" -> "abcdef"). Tokens shorter than 6 characters are
// returned unchanged. Callers must never log or return the full token.
func Suffix(token string) string {
	if len(token) <= 6 {
		return token
	}
	return token[len(token)-6:]
}
