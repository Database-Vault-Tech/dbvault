// Package encryption contains DBVault's cryptography. It deliberately uses
// only well-established primitives and formats:
//
//   - Secrets at rest (database passwords, S3 keys, webhook secrets, backup
//     private keys) are sealed with AES-256-GCM using the master
//     ENCRYPTION_KEY, with a random 96-bit nonce and associated data that
//     binds each ciphertext to its purpose, so a ciphertext cannot be moved
//     into a different column or row.
//   - Backup streams are encrypted with age (https://age-encryption.org), a
//     widely reviewed streaming authenticated-encryption format
//     (X25519 + ChaCha20-Poly1305 STREAM). Encrypted backups can be decrypted
//     with the standard `age` CLI, so there is no lock-in.
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const sealedPrefix = "v1."

// ErrDecrypt is returned for any authentication or format failure.
var ErrDecrypt = errors.New("decryption failed")

// Sealer encrypts small secrets with AES-256-GCM.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer builds a Sealer from a 32-byte master key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext. aad names the purpose, e.g. "database:<id>:password".
func (s *Sealer) Seal(plaintext []byte, aad string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := s.aead.Seal(nonce, nonce, plaintext, []byte(aad))
	return sealedPrefix + base64.RawURLEncoding.EncodeToString(out), nil
}

// SealString is Seal for string values.
func (s *Sealer) SealString(plaintext, aad string) (string, error) {
	return s.Seal([]byte(plaintext), aad)
}

// Open decrypts a value produced by Seal with the same aad.
func (s *Sealer) Open(sealed, aad string) ([]byte, error) {
	if !strings.HasPrefix(sealed, sealedPrefix) {
		return nil, ErrDecrypt
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(sealed, sealedPrefix))
	if err != nil || len(raw) < s.aead.NonceSize()+s.aead.Overhead() {
		return nil, ErrDecrypt
	}
	nonce, ct := raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():]
	pt, err := s.aead.Open(nil, nonce, ct, []byte(aad))
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

// OpenString is Open for string values.
func (s *Sealer) OpenString(sealed, aad string) (string, error) {
	b, err := s.Open(sealed, aad)
	return string(b), err
}
