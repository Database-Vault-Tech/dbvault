// Package sqlite is the SQLite driver. SQLite databases are files, so they
// are reached through a directory mounted into DBVault (SQLITE_ROOT) rather
// than over the network. It uses the pure-Go modernc.org/sqlite library, so
// the worker needs no sqlite3 binary.
//
// Backups are VACUUM INTO snapshots: a consistent, compacted copy of the
// database taken while applications keep reading and writing it. The stored
// artifact is an ordinary SQLite file that opens with any SQLite tool once
// decrypted and decompressed.
package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver

	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/validate"
)

// Driver implements engine.Driver for SQLite database files.
type Driver struct {
	// Root is the directory holding the database files (SQLITE_ROOT). Empty
	// disables SQLite on this instance.
	Root string
	// WorkDir holds short-lived snapshots.
	WorkDir string
}

func New(root, workDir string) *Driver { return &Driver{Root: root, WorkDir: workDir} }

var (
	_ engine.Driver       = (*Driver)(nil)
	_ engine.Availability = (*Driver)(nil)
)

func (d *Driver) Name() string           { return engine.SQLite }
func (d *Driver) Label() string          { return "SQLite" }
func (d *Driver) DefaultPort() int       { return 0 }
func (d *Driver) SSLModes() []string     { return []string{"disable"} }
func (d *Driver) DefaultSSLMode() string { return "disable" }
func (d *Driver) FileExtension() string  { return ".db" }
func (d *Driver) Format() string         { return "sqlite_file" }
func (d *Driver) Capabilities() engine.Capabilities {
	// A restore writes a complete file and renames it into place, so a
	// failed restore leaves the target untouched.
	return engine.Capabilities{AtomicRestore: true, FileBased: true}
}

const notEnabled = "SQLite is not enabled on this DBVault instance. Set SQLITE_ROOT to a folder containing your database files " +
	"and mount it into the api and worker containers (see docs/engines.md)"

// Unavailable reports why SQLite can't be used on this instance, or "".
func (d *Driver) Unavailable() string {
	if d.Root == "" {
		return notEnabled
	}
	if fi, err := os.Stat(d.Root); err != nil || !fi.IsDir() {
		return fmt.Sprintf("The SQLite folder %s (SQLITE_ROOT) doesn't exist or isn't mounted", d.Root)
	}
	return ""
}

// resolve maps a relative database path onto the filesystem, refusing
// anything (including symlinks) that would leave the SQLite folder.
func (d *Driver) resolve(t engine.Target, name string) (string, error) {
	root := t.Dir
	if root == "" {
		root = d.Root
	}
	if root == "" {
		return "", errors.New(notEnabled)
	}
	if !validate.IsRelativeFilePath(name) {
		return "", fmt.Errorf("%q is not a valid path inside the SQLite folder", name)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("the SQLite folder %s isn't accessible: %w", root, err)
	}
	p := filepath.Join(realRoot, filepath.FromSlash(name))
	check := p
	if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
		check = filepath.Dir(p) // new file: its folder must exist and be inside
	}
	real, err := filepath.EvalSymlinks(check)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("folder %q doesn't exist in the SQLite folder", path.Dir(name))
	}
	if err != nil {
		return "", err
	}
	if real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("%q points outside the SQLite folder", name)
	}
	if check == p {
		return real, nil
	}
	return filepath.Join(real, filepath.Base(p)), nil
}

var sqliteHeader = []byte("SQLite format 3\x00")

// checkFile confirms p is a readable SQLite database (an empty file is a
// valid empty database).
func checkFile(p, name string) (os.FileInfo, error) {
	f, err := os.Open(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("%s wasn't found in the SQLite folder", name)
	case errors.Is(err, fs.ErrPermission):
		return nil, permissionError(name)
	case err != nil:
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("%s is a folder, not a database file", name)
	}
	if fi.Size() == 0 {
		return fi, nil
	}
	head := make([]byte, len(sqliteHeader))
	if _, err := io.ReadFull(f, head); err != nil || !bytes.Equal(head, sqliteHeader) {
		return nil, fmt.Errorf("%s is not a SQLite 3 database", name)
	}
	return fi, nil
}

func permissionError(name string) error {
	return fmt.Errorf("DBVault can't access %s: permission denied. The DBVault containers run as uid 10001; "+
		"give that user read and write access to the file and its folder", name)
}

// open opens a database file. Read-only connections never modify the file
// (though SQLite may create the -shm file of a WAL database).
func open(p string, readOnly bool) (*sql.DB, error) {
	q := "_pragma=busy_timeout(10000)"
	if readOnly {
		q = "mode=ro&" + q
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(p), RawQuery: q}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// parseVersion turns "3.50.4" into (3, 3050004), SQLite's own numbering.
func parseVersion(v string) (major, num int) {
	parts := strings.SplitN(v, ".", 3)
	nums := [3]int{}
	for i := 0; i < len(parts) && i < 3; i++ {
		nums[i], _ = strconv.Atoi(parts[i])
	}
	return nums[0], nums[0]*1_000_000 + nums[1]*1_000 + nums[2]
}

const listTables = `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`

func tableNames(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, listTables)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func quoteIdent(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// Inspect opens the file read-only and reports its facts. Version is the
// SQLite library version DBVault uses to read it.
func (d *Driver) Inspect(ctx context.Context, t engine.Target) (engine.ServerInfo, error) {
	start := time.Now()
	p, err := d.resolve(t, t.Database)
	if err != nil {
		return engine.ServerInfo{}, err
	}
	fi, err := checkFile(p, t.Database)
	if err != nil {
		return engine.ServerInfo{}, err
	}
	db, err := open(p, true)
	if err != nil {
		return engine.ServerInfo{}, err
	}
	defer db.Close()
	var info engine.ServerInfo
	if err := db.QueryRowContext(ctx, `SELECT sqlite_version()`).Scan(&info.Version); err != nil {
		return info, friendly(t.Database, err)
	}
	tables, err := tableNames(ctx, db)
	if err != nil {
		return info, friendly(t.Database, err)
	}
	info.TableCount = len(tables)
	info.Major, info.VersionNum = parseVersion(info.Version)
	info.SizeBytes = fi.Size()
	info.FullVersion = "SQLite " + info.Version
	info.LatencyMilli = time.Since(start).Milliseconds()
	return info, nil
}

func friendly(name string, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "file is not a database"):
		return fmt.Errorf("%s is not a SQLite database (or is encrypted)", name)
	case strings.Contains(msg, "unable to open database file"), strings.Contains(msg, "readonly"):
		return fmt.Errorf("SQLite couldn't open %s: %w. If it uses WAL mode, DBVault also needs write access to its folder for the -shm file", name, err)
	}
	return err
}

// Dump takes a VACUUM INTO snapshot in the work directory and streams it.
func (d *Driver) Dump(ctx context.Context, t engine.Target, server engine.ServerInfo) (*engine.Dump, error) {
	p, err := d.resolve(t, t.Database)
	if err != nil {
		return nil, err
	}
	if _, err := checkFile(p, t.Database); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(d.WorkDir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(d.WorkDir, "sqlite-snapshot-*.db")
	if err != nil {
		return nil, err
	}
	snap := f.Name()
	f.Close()
	db, err := open(p, true)
	if err != nil {
		os.Remove(snap)
		return nil, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, snap); err != nil {
		os.Remove(snap)
		return nil, fmt.Errorf("snapshot of %s failed: %w", t.Database, friendly(t.Database, err))
	}
	fh, err := os.Open(snap)
	if err != nil {
		os.Remove(snap)
		return nil, err
	}
	return &engine.Dump{
		Stream: fh,
		Wait: func() error {
			fh.Close()
			return os.Remove(snap)
		},
		ToolVersion: "sqlite " + server.Version,
	}, nil
}

func (d *Driver) CheckRestoreTool(ctx context.Context) error { return nil }

// spool writes an archive to a private temporary file so SQLite can open it.
func (d *Driver) spool(archive io.Reader) (string, error) {
	if err := os.MkdirAll(d.WorkDir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(d.WorkDir, "sqlite-archive-*.db")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, archive); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// Tables lists the tables in a snapshot.
func (d *Driver) Tables(ctx context.Context, archive io.Reader) ([]engine.Table, error) {
	p, err := d.spool(archive)
	if err != nil {
		return nil, err
	}
	defer os.Remove(p)
	if _, err := checkFile(p, "the backup"); err != nil {
		return nil, err
	}
	db, err := open(p, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	names, err := tableNames(ctx, db)
	if err != nil {
		return nil, friendly("the backup", err)
	}
	out := make([]engine.Table, len(names))
	for i, n := range names {
		out[i] = engine.Table{Name: n}
	}
	return out, nil
}

// integrity runs SQLite's integrity check ("quick" skips index contents).
func integrity(ctx context.Context, p string, quick bool) error {
	db, err := open(p, true)
	if err != nil {
		return err
	}
	defer db.Close()
	pragma := "PRAGMA integrity_check(20)"
	if quick {
		pragma = "PRAGMA quick_check(20)"
	}
	rows, err := db.QueryContext(ctx, pragma)
	if err != nil {
		return err
	}
	defer rows.Close()
	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return err
		}
		if line != "ok" {
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("SQLite integrity check failed: %s", strings.Join(problems, "; "))
	}
	return nil
}

// sidecars are the files SQLite keeps next to a database.
func sidecars(p string) []string { return []string{p + "-wal", p + "-shm", p + "-journal"} }

// checkIdle refuses to replace a database that an application has open or
// that wasn't closed cleanly: its WAL or hot journal would otherwise be
// applied to (and corrupt) the restored file.
func checkIdle(p, name string) error {
	for _, s := range []string{p + "-wal", p + "-journal"} {
		if fi, err := os.Stat(s); err == nil && fi.Size() > 0 {
			return fmt.Errorf("%s is in use (%s is not empty). Stop the application using it, then restore again, "+
				"or restore into a new file", name, filepath.Base(s))
		}
	}
	return nil
}

// Restore writes the snapshot next to the target, checks it, and renames it
// into place, so the target is either fully replaced or left untouched.
func (d *Driver) Restore(ctx context.Context, t engine.Target, dbName string, archive io.Reader, o engine.RestoreOptions, log engine.Logger) error {
	dest, err := d.resolve(t, dbName)
	if err != nil {
		return err
	}
	st, statErr := os.Stat(dest)
	exists := statErr == nil
	if exists && st.IsDir() {
		return fmt.Errorf("%s is a folder", dbName)
	}
	if exists && !o.Overwrite {
		return fmt.Errorf("%s already exists in the SQLite folder", dbName)
	}
	if exists {
		if err := checkIdle(dest, dbName); err != nil {
			return err
		}
	}
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dest)+".dbvault-restore-*")
	if errors.Is(err, fs.ErrPermission) {
		return permissionError(path.Dir(dbName))
	}
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	n, err := io.Copy(tmp, archive)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("writing the restored file: %w", err)
	}
	log.Infof("Wrote %d bytes; checking the restored file's integrity", n)
	if err := integrity(ctx, tmp.Name(), true); err != nil {
		return err
	}

	// Keep the file's ownership and permissions (or its folder's, for a
	// new file) so the application can still open it.
	mode := fs.FileMode(0o640)
	owner := st
	if exists {
		mode = st.Mode().Perm()
	} else if fi, err := os.Stat(dir); err == nil {
		owner = fi
	}
	_ = os.Chmod(tmp.Name(), mode)
	if owner != nil {
		chownLike(tmp.Name(), owner)
	}
	if exists {
		for _, s := range sidecars(dest) {
			_ = os.Remove(s) // empty, per checkIdle; stale ones must not survive
		}
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return err
	}
	syncDir(dir)
	if exists {
		log.Infof("Replaced %s", dbName)
	} else {
		log.Infof("Created %s", dbName)
	}
	return nil
}

func syncDir(dir string) {
	if f, err := os.Open(dir); err == nil {
		_ = f.Sync()
		f.Close()
	}
}

func (d *Driver) DatabaseExists(ctx context.Context, t engine.Target, name string) (bool, error) {
	p, err := d.resolve(t, name)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// CreateDatabase checks that name can be created. Restore creates the file
// itself (atomically), so nothing is written here.
func (d *Driver) CreateDatabase(ctx context.Context, t engine.Target, name string) error {
	exists, err := d.DatabaseExists(ctx, t, name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%s already exists in the SQLite folder", name)
	}
	return nil
}

// DropDatabase removes a database file and its sidecar files.
func (d *Driver) DropDatabase(ctx context.Context, t engine.Target, name string) error {
	p, err := d.resolve(t, name)
	if err != nil {
		return err
	}
	for _, f := range append([]string{p}, sidecars(p)...) {
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// CheckTables confirms tables exist; with countRows (restore tests) it also
// counts every row and runs SQLite's full integrity check.
func (d *Driver) CheckTables(ctx context.Context, t engine.Target, dbName string, tables []engine.Table, countRows bool) (engine.TableCheck, error) {
	check := engine.TableCheck{Expected: len(tables)}
	p, err := d.resolve(t, dbName)
	if err != nil {
		return check, err
	}
	db, err := open(p, true)
	if err != nil {
		return check, err
	}
	defer db.Close()
	names, err := tableNames(ctx, db)
	if err != nil {
		return check, friendly(dbName, err)
	}
	present := make(map[string]bool, len(names))
	for _, n := range names {
		present[n] = true
	}
	for _, tb := range tables {
		if !present[tb.Name] {
			check.Missing = append(check.Missing, tb.String())
			continue
		}
		check.Found++
		if countRows {
			var n int64
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+quoteIdent(tb.Name)).Scan(&n); err != nil {
				return check, fmt.Errorf("counting rows in %s: %w", tb.Name, err)
			}
			check.Rows += n
		}
	}
	if countRows {
		if err := integrity(ctx, p, false); err != nil {
			return check, err
		}
	}
	return check, nil
}

// Sandbox is unused: SQLite restore tests use a temporary directory on the
// worker instead of a container.
func (d *Driver) Sandbox(major int, password string) engine.SandboxSpec { return engine.SandboxSpec{} }
