package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbvault/dbvault/backend/internal/engine"
)

type logSink struct{ t *testing.T }

func (l logSink) Infof(f string, a ...any) { l.t.Logf("INFO  "+f, a...) }
func (l logSink) Warnf(f string, a ...any) { l.t.Logf("WARN  "+f, a...) }

// fixture creates root/app/shop.db in WAL mode with two tables and returns
// an open read-write handle to it.
func fixture(t *testing.T) (*Driver, *sql.DB) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := open(filepath.Join(root, "app", "shop.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT NOT NULL, photo BLOB)`,
		`CREATE TABLE "order items" (id INTEGER PRIMARY KEY, customer_id INTEGER REFERENCES customers(id), total REAL)`,
		`CREATE INDEX items_customer ON "order items"(customer_id)`,
		`CREATE VIEW big AS SELECT * FROM "order items" WHERE total > 50`,
		`INSERT INTO customers (name, photo) VALUES ('Ann', x'00ff10'), ('Zoë ✓', x'deadbeef'), ('Bob "quoted"', NULL)`,
		`WITH RECURSIVE s(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM s WHERE n < 500)
		 INSERT INTO "order items" (customer_id, total) SELECT 1 + n % 3, n / 7.0 FROM s`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return New(root, t.TempDir()), db
}

func TestResolveStaysInsideRoot(t *testing.T) {
	d, _ := fixture(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(d.Root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(d.Root, "app", "shop.db"), filepath.Join(d.Root, "alias.db")); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{
		"../secret.db":         "not a valid path",
		"/etc/passwd":          "not a valid path",
		"escape/secret.db":     "outside the SQLite folder",
		"escape/new.db":        "outside the SQLite folder",
		"missing/dir/x.db":     "doesn't exist",
		"app/../../outside.db": "not a valid path",
	}
	for name, want := range bad {
		if _, err := d.resolve(engine.Target{}, name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("resolve(%q) = %v, want error containing %q", name, err, want)
		}
	}
	// A symlink that stays inside the folder is fine.
	if p, err := d.resolve(engine.Target{}, "alias.db"); err != nil || !strings.HasSuffix(p, filepath.Join("app", "shop.db")) {
		t.Errorf("inside symlink: %q %v", p, err)
	}
	if _, err := New("", "").resolve(engine.Target{}, "x.db"); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("empty root must report SQLite as not enabled: %v", err)
	}
	if New("", "").Unavailable() == "" || d.Unavailable() != "" {
		t.Error("Unavailable is wrong")
	}
}

func TestInspect(t *testing.T) {
	d, _ := fixture(t)
	ctx := context.Background()
	info, err := d.Inspect(ctx, engine.Target{Database: "app/shop.db"})
	if err != nil {
		t.Fatal(err)
	}
	if info.TableCount != 2 || info.Major != 3 || info.VersionNum < 3_027_000 || info.SizeBytes == 0 {
		t.Fatalf("unexpected info %+v", info)
	}
	if err := os.WriteFile(filepath.Join(d.Root, "notes.db"), []byte("hello, not a database at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"notes.db": "not a SQLite 3 database", "app/nope.db": "wasn't found", "app": "is a folder"} {
		if _, err := d.Inspect(ctx, engine.Target{Database: name}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Inspect(%q) = %v, want %q", name, err, want)
		}
	}
}

func dump(t *testing.T, d *Driver, name string) []byte {
	t.Helper()
	ctx := context.Background()
	info, err := d.Inspect(ctx, engine.Target{Database: name})
	if err != nil {
		t.Fatal(err)
	}
	dm, err := d.Dump(ctx, engine.Target{Database: name}, info)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(dm.Stream)
	if err != nil {
		t.Fatal(err)
	}
	if err := dm.Wait(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dm.ToolVersion, "sqlite 3.") {
		t.Errorf("tool version %q", dm.ToolVersion)
	}
	return b
}

// The snapshot must be consistent while a writer holds an open
// transaction: committed data (even still in the WAL) is included,
// uncommitted data is not.
func TestDumpIsConsistentSnapshot(t *testing.T) {
	d, db := fixture(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO customers (name) VALUES ('committed, still in WAL')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO customers (name) VALUES ('uncommitted')`); err != nil {
		t.Fatal(err)
	}

	snap := dump(t, d, "app/shop.db")
	if !bytes.HasPrefix(snap, sqliteHeader) {
		t.Fatal("snapshot is not a SQLite file")
	}
	entries, _ := os.ReadDir(d.WorkDir)
	if len(entries) != 0 {
		t.Fatalf("snapshot temp files left behind: %v", entries)
	}

	tables, err := d.Tables(ctx, bytes.NewReader(snap))
	if err != nil || len(tables) != 2 || tables[0].Name != "customers" || tables[1].Name != "order items" {
		t.Fatalf("tables %v %v", tables, err)
	}
	if err := d.Restore(ctx, engine.Target{}, "app/restored.db", bytes.NewReader(snap), engine.RestoreOptions{}, logSink{t}); err != nil {
		t.Fatal(err)
	}
	check, err := d.CheckTables(ctx, engine.Target{}, "app/restored.db", tables, true)
	if err != nil || check.Found != 2 || check.Rows != 4+500 {
		t.Fatalf("check %+v %v", check, err)
	}
	r, _ := open(filepath.Join(d.Root, "app", "restored.db"), true)
	defer r.Close()
	var name string
	var photo []byte
	if err := r.QueryRow(`SELECT name, photo FROM customers WHERE id = 2`).Scan(&name, &photo); err != nil || name != "Zoë ✓" || !bytes.Equal(photo, []byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatalf("restored data differs: %q %x %v", name, photo, err)
	}
	var n int
	if err := r.QueryRow(`SELECT count(*) FROM big`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("view not restored: %d %v", n, err)
	}
	if err := r.QueryRow(`SELECT count(*) FROM customers WHERE name = 'uncommitted'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("uncommitted row leaked into the snapshot: %d %v", n, err)
	}
}

func TestRestoreRules(t *testing.T) {
	d, db := fixture(t)
	ctx := context.Background()
	snap := dump(t, d, "app/shop.db")
	tgt := engine.Target{}

	// New file: CreateDatabase writes nothing; an existing name is refused.
	if err := d.CreateDatabase(ctx, tgt, "app/new.db"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.DatabaseExists(ctx, tgt, "app/new.db"); ok {
		t.Fatal("CreateDatabase must not create the file")
	}
	if err := d.CreateDatabase(ctx, tgt, "app/shop.db"); err == nil {
		t.Fatal("existing file accepted as a new database")
	}
	if err := d.Restore(ctx, tgt, "app/shop.db", bytes.NewReader(snap), engine.RestoreOptions{}, logSink{t}); err == nil {
		t.Fatal("restore without Overwrite replaced an existing file")
	}

	// The live database has a non-empty WAL, so overwriting it is refused.
	if _, err := db.Exec(`INSERT INTO customers (name) VALUES ('wal')`); err != nil {
		t.Fatal(err)
	}
	err := d.Restore(ctx, tgt, "app/shop.db", bytes.NewReader(snap), engine.RestoreOptions{Overwrite: true}, logSink{t})
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("restore over an in-use database: %v", err)
	}
	// Once the application closes it (WAL checkpointed and removed), overwrite works.
	db.Close()
	if err := d.Restore(ctx, tgt, "app/shop.db", bytes.NewReader(snap), engine.RestoreOptions{Overwrite: true}, logSink{t}); err != nil {
		t.Fatalf("overwrite restore: %v", err)
	}

	// A corrupt archive never replaces the target.
	before, _ := os.ReadFile(filepath.Join(d.Root, "app", "shop.db"))
	bad := append([]byte(nil), snap...)
	for i := 4096; i < len(bad) && i < 4096*3; i++ {
		bad[i] ^= 0x5a
	}
	if err := d.Restore(ctx, tgt, "app/shop.db", bytes.NewReader(bad), engine.RestoreOptions{Overwrite: true}, logSink{t}); err == nil {
		t.Fatal("corrupt archive restored")
	}
	after, _ := os.ReadFile(filepath.Join(d.Root, "app", "shop.db"))
	if !bytes.Equal(before, after) {
		t.Fatal("failed restore modified the target")
	}
	if left, _ := filepath.Glob(filepath.Join(d.Root, "app", ".shop.db.dbvault-restore-*")); len(left) > 0 {
		t.Fatalf("temporary restore files left behind: %v", left)
	}

	// Restore tests use Target.Dir (a private temp dir) instead of the root.
	sandbox := engine.Target{Dir: t.TempDir()}
	if err := d.Restore(ctx, sandbox, "verify.db", bytes.NewReader(snap), engine.RestoreOptions{}, logSink{t}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sandbox.Dir, "verify.db")); err != nil {
		t.Fatal("sandbox restore must write inside Target.Dir")
	}
	if err := d.DropDatabase(ctx, sandbox, "verify.db"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.DatabaseExists(ctx, sandbox, "verify.db"); ok {
		t.Fatal("DropDatabase left the file")
	}
}

func TestParseVersion(t *testing.T) {
	if m, n := parseVersion("3.50.4"); m != 3 || n != 3050004 {
		t.Errorf("got %d %d", m, n)
	}
	if quoteIdent(`a"b`) != `"a""b"` {
		t.Error("quoteIdent")
	}
}
