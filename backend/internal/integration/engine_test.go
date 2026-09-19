// Package integration contains tests that exercise real PostgreSQL servers
// and the real pg_dump/pg_restore binaries.
//
// They are skipped unless DBVAULT_TEST_SOURCE_URL points at a disposable
// PostgreSQL server where the test may create and drop databases, e.g.
//
//	DBVAULT_TEST_SOURCE_URL=postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable
//
// scripts/test-integration.sh starts everything needed with Docker.
package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dbvault/dbvault/backend/internal/backups"
	"github.com/dbvault/dbvault/backend/internal/database"
	"github.com/dbvault/dbvault/backend/internal/encryption"
	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/engine/postgres"
	"github.com/dbvault/dbvault/backend/internal/pgtools"
	"github.com/dbvault/dbvault/backend/internal/storage"
)

type logSink struct{ t *testing.T }

func pgDriver(t *testing.T) *postgres.Driver { return postgres.New(pgtools.Tools{}, t.TempDir()) }

func drivers(t *testing.T) *engine.Registry { return engine.NewRegistry(pgDriver(t)) }

func (l logSink) Infof(f string, a ...any)  { l.t.Logf("INFO  "+f, a...) }
func (l logSink) Warnf(f string, a ...any)  { l.t.Logf("WARN  "+f, a...) }
func (l logSink) Errorf(f string, a ...any) { l.t.Logf("ERROR "+f, a...) }

// adminTarget parses DBVAULT_TEST_SOURCE_URL.
func adminTarget(t *testing.T) database.Target {
	t.Helper()
	raw := os.Getenv("DBVAULT_TEST_SOURCE_URL")
	if raw == "" {
		t.Skip("DBVAULT_TEST_SOURCE_URL not set")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		port = 5432
	}
	pw, _ := u.User.Password()
	ssl := u.Query().Get("sslmode")
	if ssl == "" {
		ssl = "disable"
	}
	return database.Target{Host: u.Hostname(), Port: port, Database: strings.TrimPrefix(u.Path, "/"), Username: u.User.Username(), Password: pw, SSLMode: ssl}
}

func connect(t *testing.T, target database.Target, db string) *pgx.Conn {
	t.Helper()
	m, err := postgres.Materialize(target, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	conn, err := m.Connect(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}

// createDB creates a throwaway database and drops it after the test.
func createDB(t *testing.T, admin database.Target) string {
	t.Helper()
	name := fmt.Sprintf("dbvault_it_%d", time.Now().UnixNano())
	conn := connect(t, admin, "")
	if _, err := conn.Exec(context.Background(), "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := connect(t, admin, "")
		_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return name
}

// seed creates sample data and returns expected row counts per table.
func seed(t *testing.T, admin database.Target, db string) map[string]int64 {
	t.Helper()
	conn := connect(t, admin, db)
	_, err := conn.Exec(context.Background(), `
		CREATE TABLE customers (id serial PRIMARY KEY, name text NOT NULL, email text UNIQUE);
		CREATE TABLE orders (id serial PRIMARY KEY, customer_id int REFERENCES customers(id), total numeric(10,2), note text);
		CREATE SCHEMA billing;
		CREATE TABLE billing.invoices (id bigserial PRIMARY KEY, order_id int REFERENCES orders(id), payload jsonb);
		INSERT INTO customers (name, email) SELECT 'Customer ' || g, 'c' || g || '@example.com' FROM generate_series(1, 2000) g;
		INSERT INTO orders (customer_id, total, note) SELECT 1 + (g % 2000), (g % 997)::numeric / 7, repeat('x', g % 50) FROM generate_series(1, 15000) g;
		INSERT INTO billing.invoices (order_id, payload) SELECT g, jsonb_build_object('n', g, 'tags', jsonb_build_array('a', 'b')) FROM generate_series(1, 15000) g;
		CREATE VIEW top_customers AS SELECT customer_id, sum(total) AS spent FROM orders GROUP BY 1;`)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]int64{"public.customers": 2000, "public.orders": 15000, "billing.invoices": 15000}
}

func counts(t *testing.T, admin database.Target, db string) map[string]int64 {
	t.Helper()
	conn := connect(t, admin, db)
	out := map[string]int64{}
	for _, tbl := range []string{"public.customers", "public.orders", "billing.invoices"} {
		var n int64
		if err := conn.QueryRow(context.Background(), "SELECT count(*) FROM "+tbl).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		out[tbl] = n
	}
	var checksum string
	if err := conn.QueryRow(context.Background(), `SELECT md5(string_agg(id::text || name || email, ',' ORDER BY id)) FROM customers`).Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	out["customers_md5:"+checksum] = 1
	return out
}

// TestBackupAndRestoreRoundTrip is the end-to-end proof: create sample data,
// back it up with the real pipeline (pg_dump → zstd → age → sha256 →
// storage), verify the stored checksum, restore into a fresh database, and
// compare the data.
func TestBackupAndRestoreRoundTrip(t *testing.T) {
	admin := adminTarget(t)
	ctx := context.Background()
	src := createDB(t, admin)
	seed(t, admin, src)
	want := counts(t, admin, src)

	st, err := storage.NewLocal(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	pub, ident, err := encryption.GenerateBackupKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, compression := range []string{"zstd", "gzip"} {
		t.Run(compression, func(t *testing.T) {
			target := admin
			target.Database = src
			eng := &backups.Engine{Drivers: drivers(t), VerifyUpload: true}
			key := backups.ObjectKey("it", src, time.Now(), ".dump", compression, true, compression)
			res, err := eng.Run(ctx, backups.Request{Target: target, Storage: st, Key: key, Compression: compression, PublicKey: pub, Log: logSink{t}})
			if err != nil {
				t.Fatalf("backup failed: %v", err)
			}
			if res.SizeBytes == 0 || res.RawSizeBytes <= res.SizeBytes || len(res.Checksum) != 64 || res.TableCount != 3 {
				t.Fatalf("unexpected result %+v", res)
			}

			// Download with checksum verification, decrypt, decompress, restore.
			path, _, err := backups.DownloadVerified(ctx, st, key, res.Checksum, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			archive, err := backups.OpenArchive(f, compression, ident)
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()

			dst := createDB(t, admin)
			if err := pgDriver(t).Restore(ctx, admin, dst, archive, engine.RestoreOptions{Atomic: true}, logSink{t}); err != nil {
				t.Fatalf("restore: %v", err)
			}
			got := counts(t, admin, dst)
			for k, v := range want {
				if got[k] != v {
					t.Fatalf("restored data differs for %s: got %d want %d", k, got[k], v)
				}
			}
			// The view must come back too.
			c := connect(t, admin, dst)
			var n int
			if err := c.QueryRow(ctx, "SELECT count(*) FROM top_customers").Scan(&n); err != nil || n == 0 {
				t.Fatalf("view not restored: %v", err)
			}
		})
	}
}

func TestBackupFailsWithBadCredentials(t *testing.T) {
	admin := adminTarget(t)
	st, _ := storage.NewLocal(t.TempDir(), "")
	target := admin
	target.Password = "definitely-wrong"
	eng := &backups.Engine{Drivers: drivers(t)}
	_, err := eng.Run(context.Background(), backups.Request{Target: target, Storage: st, Key: "x/backup.dump", Compression: "zstd", Log: logSink{t}})
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("expected authentication error, got %v", err)
	}
	if list, _ := st.List(context.Background(), ""); len(list) != 0 {
		t.Fatalf("failed backup left objects: %v", list)
	}
}

// failingStorage accepts some bytes then fails, like an S3 outage mid-upload.
type failingStorage struct {
	storage.Storage
	after int64
}

func (f failingStorage) Upload(ctx context.Context, key string, r io.Reader) (int64, error) {
	n, err := io.CopyN(io.Discard, r, f.after)
	if err != nil {
		return n, err
	}
	return n, errors.New("connection reset by storage provider")
}

func TestBackupStorageFailureStopsPgDump(t *testing.T) {
	admin := adminTarget(t)
	src := createDB(t, admin)
	seed(t, admin, src)
	target := admin
	target.Database = src
	local, _ := storage.NewLocal(t.TempDir(), "")
	eng := &backups.Engine{Drivers: drivers(t)}
	_, err := eng.Run(context.Background(), backups.Request{Target: target, Storage: failingStorage{Storage: local, after: 1024},
		Key: "x/backup.dump", Compression: "none", Log: logSink{t}})
	var se *backups.StorageError
	if !errors.As(err, &se) {
		t.Fatalf("expected StorageError, got %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	out, _ := exec.Command("pgrep", "-f", "pg_dump --format=custom").Output()
	if len(strings.TrimSpace(string(out))) > 0 {
		t.Fatalf("pg_dump still running after storage failure: %s", out)
	}
}

func TestCorruptedBackupIsDetected(t *testing.T) {
	admin := adminTarget(t)
	src := createDB(t, admin)
	seed(t, admin, src)
	target := admin
	target.Database = src
	root := t.TempDir()
	st, _ := storage.NewLocal(root, "")
	eng := &backups.Engine{Drivers: drivers(t), VerifyUpload: true}
	res, err := eng.Run(context.Background(), backups.Request{Target: target, Storage: st, Key: "x/backup.dump.zst", Compression: "zstd", Log: logSink{t}})
	if err != nil {
		t.Fatal(err)
	}
	// Flip one byte in the stored artifact.
	p := root + "/x/backup.dump.zst"
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)/2] ^= 0xff
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = backups.DownloadVerified(context.Background(), st, "x/backup.dump.zst", res.Checksum, t.TempDir())
	var ce *backups.ChecksumError
	if !errors.As(err, &ce) {
		t.Fatalf("expected checksum error, got %v", err)
	}
}
