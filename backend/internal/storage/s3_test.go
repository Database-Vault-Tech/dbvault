package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// S3 tests run against a real S3-compatible server (MinIO in CI):
//
//	DBVAULT_TEST_S3_ENDPOINT=http://localhost:9000 DBVAULT_TEST_S3_BUCKET=test \
//	DBVAULT_TEST_S3_ACCESS_KEY=... DBVAULT_TEST_S3_SECRET_KEY=... go test ./internal/storage
func s3FromEnv(t *testing.T) *S3 {
	t.Helper()
	ep := os.Getenv("DBVAULT_TEST_S3_ENDPOINT")
	if ep == "" {
		t.Skip("DBVAULT_TEST_S3_ENDPOINT not set")
	}
	s, err := NewS3(context.Background(), TypeMinIO, Config{Endpoint: ep, Bucket: os.Getenv("DBVAULT_TEST_S3_BUCKET")},
		Credentials{AccessKeyID: os.Getenv("DBVAULT_TEST_S3_ACCESS_KEY"), SecretAccessKey: os.Getenv("DBVAULT_TEST_S3_SECRET_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestS3SmallObject(t *testing.T) {
	s := s3FromEnv(t)
	ctx := context.Background()
	key := "test/" + time.Now().Format("150405.000000") + "/small.bin"
	if err := Probe(ctx, s, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upload(ctx, key, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	defer s.Delete(ctx, key)
	obj, err := s.Stat(ctx, key)
	if err != nil || obj.Size != 5 {
		t.Fatalf("stat %+v %v", obj, err)
	}
	list, err := s.List(ctx, key)
	if err != nil || len(list) != 1 {
		t.Fatalf("list %+v %v", list, err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Exists(ctx, key); ok {
		t.Fatal("object should be gone")
	}
	if _, err := s.Download(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestS3MultipartStreaming(t *testing.T) {
	s := s3FromEnv(t)
	s.minPartSize = 5 << 20 // force several parts
	ctx := context.Background()
	key := "test/" + time.Now().Format("150405.000000") + "/multipart.bin"
	data := make([]byte, 12<<20+123)
	_, _ = rand.Read(data)
	n, err := s.Upload(ctx, key, io.MultiReader(bytes.NewReader(data))) // non-seekable reader
	if err != nil {
		t.Fatal(err)
	}
	defer s.Delete(ctx, key)
	if n != int64(len(data)) {
		t.Fatalf("uploaded %d, want %d", n, len(data))
	}
	rc, err := s.Download(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(data)
	if !bytes.Equal(h.Sum(nil), want[:]) {
		t.Fatal("downloaded data differs")
	}
}

type errAfter struct {
	left int
}

func (e *errAfter) Read(p []byte) (int, error) {
	if e.left <= 0 {
		return 0, errors.New("upstream failed")
	}
	n := len(p)
	if n > e.left {
		n = e.left
	}
	e.left -= n
	return n, nil
}

func TestS3FailedMultipartLeavesNothing(t *testing.T) {
	s := s3FromEnv(t)
	s.minPartSize = 5 << 20
	ctx := context.Background()
	key := "test/" + time.Now().Format("150405.000000") + "/aborted.bin"
	if _, err := s.Upload(ctx, key, &errAfter{left: 11 << 20}); err == nil {
		t.Fatal("expected upload error")
	}
	if ok, _ := s.Exists(ctx, key); ok {
		t.Fatal("aborted upload must not leave an object")
	}
}

func TestS3UnreachableEndpoint(t *testing.T) {
	s, err := NewS3(context.Background(), TypeMinIO, Config{Endpoint: "http://127.0.0.1:1", Bucket: "nope"},
		Credentials{AccessKeyID: "a", SecretAccessKey: "b"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = s.Upload(ctx, "k", strings.NewReader("x"))
	if err == nil || !strings.Contains(err.Error(), "upload") {
		t.Fatalf("expected upload error, got %v", err)
	}
}

func TestS3ConfigValidation(t *testing.T) {
	ctx := context.Background()
	creds := Credentials{AccessKeyID: "a", SecretAccessKey: "b"}
	if _, err := NewS3(ctx, TypeS3, Config{Bucket: "b"}, creds); err == nil {
		t.Error("S3 without region must fail")
	}
	if _, err := NewS3(ctx, TypeR2, Config{Bucket: "b"}, creds); err == nil {
		t.Error("R2 without account id must fail")
	}
	if _, err := NewS3(ctx, TypeMinIO, Config{Bucket: "b"}, creds); err == nil {
		t.Error("MinIO without endpoint must fail")
	}
	if _, err := NewS3(ctx, TypeS3, Config{Bucket: "b", Region: "eu-west-1"}, Credentials{}); err == nil {
		t.Error("missing credentials must fail")
	}
	r2, err := NewS3(ctx, TypeR2, Config{Bucket: "b", AccountID: strings.Repeat("a", 32)}, creds)
	if err != nil || r2.Describe() != "r2://b" {
		t.Errorf("R2 config: %v", err)
	}
}
