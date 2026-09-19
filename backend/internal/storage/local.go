package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Local stores backups on the server's filesystem, confined to a root directory.
type Local struct {
	dir string
}

// NewLocal returns a Local store rooted at root/sub. sub must stay inside root.
func NewLocal(root, sub string) (*Local, error) {
	if root == "" {
		return nil, errors.New("LOCAL_STORAGE_ROOT is not configured")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	dir, err := safeJoin(absRoot, sub)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}
	return &Local{dir: dir}, nil
}

// safeJoin joins rel under base, rejecting traversal outside base.
func safeJoin(base, rel string) (string, error) {
	if filepath.IsAbs(rel) || strings.Contains(rel, "\x00") {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	p := filepath.Join(base, filepath.FromSlash(rel))
	if p != base && !strings.HasPrefix(p, base+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes storage root", rel)
	}
	return p, nil
}

func (l *Local) path(key string) (string, error) {
	if key == "" || strings.HasSuffix(key, "/") {
		return "", fmt.Errorf("invalid key %q", key)
	}
	return safeJoin(l.dir, key)
}

func (l *Local) Describe() string { return "file://" + l.dir }

func (l *Local) Upload(ctx context.Context, key string, r io.Reader) (int64, error) {
	p, err := l.path(key)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".upload-*")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	fail := func(err error) (int64, error) {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return 0, err
	}
	n, err := io.Copy(tmp, ctxReader{ctx: ctx, r: r})
	if err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return 0, err
	}
	// Rename is atomic: readers never observe a partially written backup.
	if err := os.Rename(tmpName, p); err != nil {
		_ = os.Remove(tmpName)
		return 0, err
	}
	return n, nil
}

func (l *Local) Download(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

func (l *Local) Delete(_ context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (l *Local) Exists(ctx context.Context, key string) (bool, error) {
	_, err := l.Stat(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (l *Local) Stat(_ context.Context, key string) (Object, error) {
	p, err := l.path(key)
	if err != nil {
		return Object{}, err
	}
	fi, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return Object{}, ErrNotFound
	}
	if err != nil {
		return Object{}, err
	}
	return Object{Key: key, Size: fi.Size(), LastModified: fi.ModTime()}, nil
}

func (l *Local) List(_ context.Context, prefix string) ([]Object, error) {
	var out []Object
	err := filepath.WalkDir(l.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".upload-") {
			return nil
		}
		rel, err := filepath.Rel(l.dir, p)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if !strings.HasPrefix(key, prefix) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, Object{Key: key, Size: fi.Size(), LastModified: fi.ModTime()})
		return nil
	})
	return out, err
}

// ctxReader aborts long copies promptly when ctx is cancelled.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
