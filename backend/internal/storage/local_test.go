package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := NewLocal(root, "team-a")
	if err != nil {
		t.Fatal(err)
	}
	key := "production/2026/09/19/backup.dump.zst"
	n, err := s.Upload(ctx, key, strings.NewReader("payload"))
	if err != nil || n != 7 {
		t.Fatalf("upload: %d %v", n, err)
	}
	if ok, _ := s.Exists(ctx, key); !ok {
		t.Fatal("object should exist")
	}
	obj, err := s.Stat(ctx, key)
	if err != nil || obj.Size != 7 {
		t.Fatalf("stat: %+v %v", obj, err)
	}
	rc, err := s.Download(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "payload" {
		t.Fatalf("got %q", b)
	}
	list, err := s.List(ctx, "production/")
	if err != nil || len(list) != 1 || list[0].Key != key {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Download(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatal("deleting a missing object must be idempotent")
	}
}

func TestLocalRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := NewLocal(root, "../outside"); err == nil {
		t.Fatal("sub path escaping root must be rejected")
	}
	if _, err := NewLocal(root, "/etc"); err == nil {
		t.Fatal("absolute sub path must be rejected")
	}
	s, _ := NewLocal(root, "ok")
	for _, key := range []string{"../../escape", "../x", "a/../../b"} {
		if _, err := s.Upload(context.Background(), key, strings.NewReader("x")); err == nil {
			t.Errorf("key %q must be rejected", key)
		}
	}
}

type failingReader struct{ n int }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.n > 0 {
		f.n--
		return copy(p, "partial data"), nil
	}
	return 0, errors.New("pg_dump crashed")
}

func TestLocalFailedUploadLeavesNothing(t *testing.T) {
	root := t.TempDir()
	s, _ := NewLocal(root, "")
	if _, err := s.Upload(context.Background(), "db/backup.dump", &failingReader{n: 3}); err == nil {
		t.Fatal("expected error")
	}
	var files []string
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, _ error) error {
		if fi != nil && !fi.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if len(files) != 0 {
		t.Fatalf("partial files left behind: %v", files)
	}
}

func TestJoinKey(t *testing.T) {
	cases := map[[2]string]string{
		{"", "a/b"}:        "a/b",
		{"prefix", "a/b"}:  "prefix/a/b",
		{"/prefix/", "/a"}: "prefix/a",
	}
	for in, want := range cases {
		if got := JoinKey(in[0], in[1]); got != want {
			t.Errorf("JoinKey(%q,%q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
