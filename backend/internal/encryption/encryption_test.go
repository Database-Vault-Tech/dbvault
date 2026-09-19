package encryption

import (
	"bytes"
	"crypto/rand"
	"io"
	"strings"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealerRoundTrip(t *testing.T) {
	s, err := NewSealer(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.SealString("hunter2 & friends", "database:1:password")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "hunter2") {
		t.Fatal("ciphertext contains plaintext")
	}
	got, err := s.OpenString(sealed, "database:1:password")
	if err != nil || got != "hunter2 & friends" {
		t.Fatalf("round trip failed: %q %v", got, err)
	}
}

func TestSealerNonceIsRandom(t *testing.T) {
	s, _ := NewSealer(testKey(t))
	a, _ := s.SealString("same", "ctx")
	b, _ := s.SealString("same", "ctx")
	if a == b {
		t.Fatal("two encryptions of the same plaintext must differ")
	}
}

func TestSealerRejectsWrongContext(t *testing.T) {
	s, _ := NewSealer(testKey(t))
	sealed, _ := s.SealString("secret", "database:1:password")
	if _, err := s.OpenString(sealed, "database:2:password"); err == nil {
		t.Fatal("ciphertext moved to another row must not decrypt")
	}
}

func TestSealerDetectsTampering(t *testing.T) {
	s, _ := NewSealer(testKey(t))
	sealed, _ := s.SealString("secret", "ctx")
	b := []byte(sealed)
	b[len(b)-2] ^= 0x01
	if _, err := s.OpenString(string(b), "ctx"); err == nil {
		t.Fatal("tampered ciphertext must be rejected")
	}
	if _, err := s.OpenString("not-a-ciphertext", "ctx"); err == nil {
		t.Fatal("garbage must be rejected")
	}
}

func TestSealerRejectsWrongKey(t *testing.T) {
	a, _ := NewSealer(testKey(t))
	b, _ := NewSealer(testKey(t))
	sealed, _ := a.SealString("secret", "ctx")
	if _, err := b.OpenString(sealed, "ctx"); err == nil {
		t.Fatal("a different master key must not decrypt")
	}
}

func TestNewSealerKeyLength(t *testing.T) {
	if _, err := NewSealer(make([]byte, 16)); err == nil {
		t.Fatal("expected error for 16-byte key")
	}
}

func TestBackupStreamRoundTrip(t *testing.T) {
	pub, ident, err := GenerateBackupKey()
	if err != nil {
		t.Fatal(err)
	}
	// 3 MiB spans many 64 KiB age chunks.
	plain := make([]byte, 3<<20)
	_, _ = rand.Read(plain)

	var enc bytes.Buffer
	w, err := EncryptWriter(&enc, pub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(w, bytes.NewReader(plain)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc.Bytes(), plain[:64]) {
		t.Fatal("ciphertext contains plaintext")
	}
	r, err := DecryptReader(bytes.NewReader(enc.Bytes()), ident)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("decrypted data differs")
	}
}

func TestBackupStreamDetectsTampering(t *testing.T) {
	pub, ident, _ := GenerateBackupKey()
	var enc bytes.Buffer
	w, _ := EncryptWriter(&enc, pub)
	_, _ = w.Write(bytes.Repeat([]byte("row data "), 50000))
	_ = w.Close()
	b := enc.Bytes()
	b[len(b)/2] ^= 0xff
	r, err := DecryptReader(bytes.NewReader(b), ident)
	if err == nil {
		_, err = io.ReadAll(r)
	}
	if err == nil {
		t.Fatal("tampered backup must fail to decrypt")
	}
}

func TestBackupStreamWrongKey(t *testing.T) {
	pub, _, _ := GenerateBackupKey()
	_, other, _ := GenerateBackupKey()
	var enc bytes.Buffer
	w, _ := EncryptWriter(&enc, pub)
	_, _ = w.Write([]byte("data"))
	_ = w.Close()
	if _, err := DecryptReader(bytes.NewReader(enc.Bytes()), other); err == nil {
		t.Fatal("wrong identity must not decrypt")
	}
}
