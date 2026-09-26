package backups

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dbvault/dbvault/backend/internal/compress"
	"github.com/dbvault/dbvault/backend/internal/database"
	"github.com/dbvault/dbvault/backend/internal/encryption"
	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/storage"
)

// Logger receives user-visible progress lines.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// Progress is a snapshot of a running backup.
type Progress struct {
	Phase          string `json:"phase"`
	BytesDumped    int64  `json:"bytes_dumped"`
	BytesWritten   int64  `json:"bytes_written"`
	EstimatedTotal int64  `json:"estimated_total"`
}

// Engine executes the backup pipeline:
//
//	native dump (e.g. pg_dump) → compress → encrypt → SHA-256 → upload → verify
//
// Every stage is a streaming io.Reader/io.Writer connected in one pass, so
// memory use is bounded (about one upload part) no matter how large the
// database is. Nothing is staged on local disk.
type Engine struct {
	// Drivers provides the per-engine connection and dump logic.
	Drivers *engine.Registry
	// VerifyUpload re-reads the uploaded object and re-computes its
	// checksum before a backup is marked completed.
	VerifyUpload bool
}

type Request struct {
	Target      database.Target
	Storage     storage.Storage
	Key         string
	Compression string
	// PublicKey is the age recipient; empty disables encryption.
	PublicKey string
	Log       Logger
	// OnProgress is called at most once per second.
	OnProgress func(Progress)
}

type Result struct {
	Key           string        `json:"key"`
	SizeBytes     int64         `json:"size_bytes"`
	RawSizeBytes  int64         `json:"raw_size_bytes"`
	Checksum      string        `json:"checksum_sha256"`
	PGVersion     string        `json:"pg_version"`
	PGMajor       int           `json:"pg_major"`
	PGDumpVersion string        `json:"pg_dump_version"`
	TableCount    int           `json:"table_count"`
	Duration      time.Duration `json:"duration"`
}

// Run executes one backup. On failure nothing is left in storage.
func (e *Engine) Run(ctx context.Context, req Request) (Result, error) {
	start := time.Now()
	log := req.Log
	var res Result

	drv, err := e.Drivers.For(req.Target)
	if err != nil {
		return res, err
	}
	if drv.Capabilities().FileBased {
		log.Infof("Opening %s database %s", drv.Label(), req.Target.Database)
	} else {
		log.Infof("Connecting to %s at %s:%d", drv.Label(), req.Target.Host, req.Target.Port)
	}
	info, err := drv.Inspect(ctx, req.Target)
	if err != nil {
		return res, fmt.Errorf("connect: %w", err)
	}
	res.PGVersion, res.PGMajor, res.TableCount = info.Version, info.Major, info.TableCount
	log.Infof("Connection successful (%s %s, %s, %d tables)", drv.Label(), info.Version, HumanBytes(info.SizeBytes), info.TableCount)

	dumpCtx, cancelDump := context.WithCancel(ctx)
	defer cancelDump()
	dump, err := drv.Dump(dumpCtx, req.Target, info)
	if err != nil {
		return res, err
	}
	res.PGDumpVersion = dump.ToolVersion
	log.Infof("Started %s dump (tool %s)", drv.Label(), dump.ToolVersion)

	var dumped, written atomic.Int64
	pr, pw := io.Pipe()
	hasher := sha256.New()
	sink := &countingWriter{w: io.MultiWriter(pw, hasher), n: &written}

	// Progress reporting.
	stopProgress := make(chan struct{})
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-stopProgress:
				return
			case <-t.C:
				if req.OnProgress != nil {
					req.OnProgress(Progress{Phase: "dumping", BytesDumped: dumped.Load(), BytesWritten: written.Load(), EstimatedTotal: info.SizeBytes})
				}
			}
		}
	}()

	// Producer: pg_dump stdout → compressor → encryptor → (hash, pipe).
	producerErr := make(chan error, 1)
	go func() {
		producerErr <- e.produce(cancelDump, dump, sink, pw, &dumped, req)
	}()

	// Consumer: stream the pipe into storage.
	log.Infof("Uploading to %s", req.Storage.Describe())
	uploaded, uploadErr := req.Storage.Upload(ctx, req.Key, pr)
	if uploadErr != nil {
		// Unblock the producer (its writes now fail) and stop pg_dump.
		_ = pr.CloseWithError(uploadErr)
		cancelDump()
	}
	prodErr := <-producerErr
	close(stopProgress)
	<-progressDone

	switch {
	case prodErr != nil && !errors.Is(prodErr, uploadErr):
		// The dump side failed first; the upload error (if any) is only a
		// consequence. Make sure no partial object remains.
		_ = req.Storage.Delete(context.WithoutCancel(ctx), req.Key)
		return res, prodErr
	case uploadErr != nil:
		_ = req.Storage.Delete(context.WithoutCancel(ctx), req.Key)
		return res, &StorageError{Err: uploadErr}
	}

	res.Key = req.Key
	res.RawSizeBytes = dumped.Load()
	res.SizeBytes = written.Load()
	res.Checksum = hex.EncodeToString(hasher.Sum(nil))
	if uploaded != res.SizeBytes {
		_ = req.Storage.Delete(context.WithoutCancel(ctx), req.Key)
		return res, &StorageError{Err: fmt.Errorf("storage accepted %d bytes but %d were produced", uploaded, res.SizeBytes)}
	}
	log.Infof("Dump completed (%s of dump data)", HumanBytes(res.RawSizeBytes))
	if req.Compression != compress.None {
		log.Infof("Compression completed with %s (%s, %.1fx)", req.Compression, HumanBytes(res.SizeBytes), ratio(res.RawSizeBytes, res.SizeBytes))
	}
	if req.PublicKey != "" {
		log.Infof("Encryption completed (age X25519 + ChaCha20-Poly1305)")
	}
	log.Infof("Upload completed (%s)", HumanBytes(res.SizeBytes))

	if e.VerifyUpload {
		if req.OnProgress != nil {
			req.OnProgress(Progress{Phase: "verifying", BytesDumped: res.RawSizeBytes, BytesWritten: res.SizeBytes, EstimatedTotal: info.SizeBytes})
		}
		if err := VerifyStoredChecksum(ctx, req.Storage, req.Key, res.Checksum, res.SizeBytes); err != nil {
			_ = req.Storage.Delete(context.WithoutCancel(ctx), req.Key)
			return res, &StorageError{Err: err}
		}
		log.Infof("Checksum verified (sha256:%s)", res.Checksum[:16])
	} else {
		obj, err := req.Storage.Stat(ctx, req.Key)
		if err != nil || obj.Size != res.SizeBytes {
			_ = req.Storage.Delete(context.WithoutCancel(ctx), req.Key)
			return res, &StorageError{Err: fmt.Errorf("uploaded object is missing or has the wrong size")}
		}
		log.Infof("Checksum recorded (sha256:%s)", res.Checksum[:16])
	}
	res.Duration = time.Since(start)
	return res, nil
}

// produce streams pg_dump output through compression and encryption into
// sink, then closes pw. pg_dump's exit status is checked before the final
// frames are flushed, so a failed dump always surfaces as an upload error
// and is never stored as a complete (but truncated) backup.
func (e *Engine) produce(cancel context.CancelFunc, dump *engine.Dump, sink io.Writer, pw *io.PipeWriter, dumped *atomic.Int64, req Request) error {
	var encW io.WriteCloser
	var dst io.Writer = sink
	fail := func(err error) error {
		cancel()
		_ = dump.Wait()
		_ = pw.CloseWithError(err)
		return err
	}
	if req.PublicKey != "" {
		w, err := encryption.EncryptWriter(sink, req.PublicKey)
		if err != nil {
			return fail(err)
		}
		encW, dst = w, w
	}
	compW, err := compress.NewWriter(req.Compression, dst)
	if err != nil {
		return fail(err)
	}
	_, copyErr := io.Copy(compW, &countingReader{r: dump.Stream, n: dumped})
	if copyErr != nil {
		// Downstream failed (upload aborted or cancelled); stop pg_dump.
		cancel()
	}
	waitErr := dump.Wait()
	switch {
	case copyErr != nil:
		// We killed pg_dump ourselves, so its exit status is irrelevant.
		err = copyErr
	case waitErr != nil:
		err = fmt.Errorf("dump failed: %w", waitErr)
	}
	if err != nil {
		_ = pw.CloseWithError(err)
		_ = compW.Close() // release encoder goroutines; its writes now fail fast
		return err
	}
	if err := compW.Close(); err != nil {
		_ = pw.CloseWithError(err)
		return err
	}
	if encW != nil {
		if err := encW.Close(); err != nil {
			_ = pw.CloseWithError(err)
			return err
		}
	}
	return pw.Close()
}

// StorageError marks failures caused by the storage destination (so the
// "storage failure" notification can be sent).
type StorageError struct{ Err error }

func (e *StorageError) Error() string { return "storage: " + e.Err.Error() }
func (e *StorageError) Unwrap() error { return e.Err }

// VerifyStoredChecksum streams an object back from storage and compares its
// SHA-256 and size with the expected values.
func VerifyStoredChecksum(ctx context.Context, st storage.Storage, key, want string, wantSize int64) error {
	rc, err := st.Download(ctx, key)
	if err != nil {
		return fmt.Errorf("read back uploaded backup: %w", err)
	}
	defer rc.Close()
	h := sha256.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return fmt.Errorf("read back uploaded backup: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if n != wantSize || got != want {
		return fmt.Errorf("checksum mismatch after upload (expected %s, got %s)", want[:16], got[:16])
	}
	return nil
}

// DownloadVerified copies an object into a private temp file while
// computing its checksum, and fails if it doesn't match. The caller must
// remove the returned file.
func DownloadVerified(ctx context.Context, st storage.Storage, key, wantChecksum, workDir string) (string, int64, error) {
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return "", 0, err
	}
	f, err := os.CreateTemp(workDir, "download-*.bin")
	if err != nil {
		return "", 0, err
	}
	path := f.Name()
	cleanup := func() { f.Close(); os.Remove(path) }
	rc, err := st.Download(ctx, key)
	if err != nil {
		cleanup()
		return "", 0, fmt.Errorf("download backup: %w", err)
	}
	defer rc.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), rc)
	if err != nil {
		cleanup()
		return "", 0, fmt.Errorf("download backup: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", 0, err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantChecksum) {
		os.Remove(path)
		return "", n, &ChecksumError{Want: wantChecksum, Got: got}
	}
	return path, n, nil
}

// ChecksumError reports a corrupted or tampered backup artifact.
type ChecksumError struct{ Want, Got string }

func (e *ChecksumError) Error() string {
	return fmt.Sprintf("checksum mismatch: expected sha256:%s, got sha256:%s (the backup is corrupted or was modified)", short(e.Want), short(e.Got))
}

func short(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

// OpenArchive returns the plain pg_dump archive from a stored artifact
// stream by decrypting (if needed) and decompressing it.
func OpenArchive(r io.Reader, compression string, identity string) (io.ReadCloser, error) {
	src := r
	if identity != "" {
		dec, err := encryption.DecryptReader(r, identity)
		if err != nil {
			return nil, fmt.Errorf("decrypt backup: %w", err)
		}
		src = dec
	}
	return compress.NewReader(compression, src)
}

// Hash is exposed for tests.
func Hash() hash.Hash { return sha256.New() }

type countingWriter struct {
	w io.Writer
	n *atomic.Int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(int64(n))
	return n, err
}

type countingReader struct {
	r io.Reader
	n *atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

func ratio(raw, stored int64) float64 {
	if stored == 0 {
		return 0
	}
	return float64(raw) / float64(stored)
}

// HumanBytes formats a byte count (1024-based) for logs.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ObjectKey builds the predictable storage key for a backup:
//
//	<prefix>/<database>/<YYYY>/<MM>/<DD>/backup_<YYYY-MM-DD_HH-MM-SS><ext>[.zst|.gz][.age]
//
// where ext is the engine's dump extension (".dump" for PostgreSQL).
//
// suffix (optional) disambiguates two backups started in the same second.
func ObjectKey(prefix, dbName string, t time.Time, ext, compression string, encrypted bool, suffix string) string {
	t = t.UTC()
	name := "backup_" + t.Format("2006-01-02_15-04-05")
	if suffix != "" {
		name += "_" + suffix
	}
	name += ext + compress.Extension(compression)
	if encrypted {
		name += ".age"
	}
	return storage.JoinKey(prefix, fmt.Sprintf("%s/%s/%s", SlugName(dbName), t.Format("2006/01/02"), name))
}

// SlugName turns a database display name into a safe path segment.
func SlugName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-.")
	if s == "" {
		s = "database"
	}
	return s
}
