// Package pgtools runs PostgreSQL's own client programs (pg_dump,
// pg_restore). DBVault never re-implements the dump format; it relies on
// the battle-tested official tooling.
package pgtools

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Tools struct {
	BinDir string
}

// Path returns the full path of a PostgreSQL client binary.
func (t Tools) Path(name string) string {
	if t.BinDir != "" {
		return filepath.Join(t.BinDir, name)
	}
	return name
}

var versionRE = regexp.MustCompile(`(\d+)(?:\.(\d+))?`)

// Version returns the major version and version string of a client binary.
func (t Tools) Version(ctx context.Context, name string) (int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, t.Path(name), "--version").Output()
	if err != nil {
		return 0, "", fmt.Errorf("%s is not available: %w", name, err)
	}
	s := strings.TrimSpace(string(out))
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return 0, s, fmt.Errorf("cannot parse %s version from %q", name, s)
	}
	major, _ := strconv.Atoi(m[1])
	full := m[1]
	if m[2] != "" {
		full += "." + m[2]
	}
	return major, full, nil
}

// CheckCompatible verifies the client can handle a server of serverMajor.
// pg_dump refuses to dump servers newer than itself.
func (t Tools) CheckCompatible(ctx context.Context, name string, serverMajor int) (string, error) {
	major, full, err := t.Version(ctx, name)
	if err != nil {
		return "", err
	}
	if major < serverMajor {
		return full, fmt.Errorf("%s %d cannot handle PostgreSQL %d: install PostgreSQL %d+ client tools on the worker (or set PG_BIN_DIR)", name, major, serverMajor, serverMajor)
	}
	return full, nil
}

// Process is a running PostgreSQL client program.
type Process struct {
	cmd    *exec.Cmd
	stderr *tailBuffer
}

// DumpArgs are the pg_dump arguments DBVault uses: custom format (so
// pg_restore can restore selectively), with pg_dump's own compression
// disabled because DBVault compresses the stream itself.
func DumpArgs() []string {
	return []string{"--format=custom", "--compress=0", "--no-password", "--lock-wait-timeout=60000"}
}

// StartDump starts pg_dump with the given libpq env and returns its stdout.
func (t Tools) StartDump(ctx context.Context, env []string) (*Process, io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, t.Path("pg_dump"), DumpArgs()...)
	return start(cmd, env, nil, true)
}

// RestoreOptions controls pg_restore.
type RestoreOptions struct {
	// Clean drops existing objects before recreating them.
	Clean bool
	// SingleTransaction makes the restore atomic: on any error the target is
	// left unchanged.
	SingleTransaction bool
	// ServerMajor and ServerSettings describe the target server. When it is
	// older than pg_restore, a compatibility pipeline is used (see compat.go).
	ServerMajor    int
	ServerSettings map[string]bool
}

// RestoreArgs builds pg_restore arguments. Ownership and privileges are not
// restored, because the target roles frequently differ from the source.
//
// The target database is selected through PGDATABASE in the child's
// environment; "--dbname=" is passed empty only to put pg_restore into
// connect mode. Keeping the name out of argv means a database name can never
// be interpreted as a libpq connection string.
func RestoreArgs(o RestoreOptions) []string {
	args := []string{"--no-owner", "--no-privileges", "--no-password", "--exit-on-error"}
	if o.Clean {
		args = append(args, "--clean", "--if-exists")
	}
	if o.SingleTransaction {
		args = append(args, "--single-transaction")
	}
	return append(args, "--dbname=")
}

// StartRestore starts pg_restore reading the archive from stdin. env must
// contain PGDATABASE for the target database.
func (t Tools) StartRestore(ctx context.Context, env []string, o RestoreOptions, stdin io.Reader) (Waiter, error) {
	if o.ServerMajor > 0 && o.ServerSettings != nil {
		if clientMajor, _, err := t.Version(ctx, "pg_restore"); err == nil && o.ServerMajor < clientMajor {
			return t.startCompatRestore(ctx, env, o, stdin)
		}
	}
	cmd := exec.CommandContext(ctx, t.Path("pg_restore"), RestoreArgs(o)...)
	p, _, err := start(cmd, env, stdin, false)
	return p, err
}

// NeedsCompat reports whether restoring into serverMajor uses the
// compatibility pipeline (for logging).
func (t Tools) NeedsCompat(ctx context.Context, serverMajor int) (bool, int) {
	clientMajor, _, err := t.Version(ctx, "pg_restore")
	return err == nil && serverMajor > 0 && serverMajor < clientMajor, clientMajor
}

// ListArchive runs `pg_restore --list` over an archive stream and returns
// the table-of-contents entries.
func (t Tools) ListArchive(ctx context.Context, archive io.Reader) ([]TOCEntry, error) {
	cmd := exec.CommandContext(ctx, t.Path("pg_restore"), "--list")
	cmd.Stdin = archive
	var stderr tailBuffer
	stderr.max = 16 << 10
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	entries := ParseTOC(out)
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("pg_restore --list failed: %s", stderr.Summary(err))
	}
	return entries, nil
}

// TOCEntry is one line of a pg_restore --list table of contents.
type TOCEntry struct {
	Type   string
	Schema string
	Name   string
}

var tocRE = regexp.MustCompile(`^\d+;\s+\d+\s+\d+\s+(.+)$`)

// knownTypes lists multi-word TOC types, longest first.
var knownTypes = []string{"TABLE DATA", "SEQUENCE SET", "SEQUENCE OWNED BY", "MATERIALIZED VIEW DATA", "MATERIALIZED VIEW", "FK CONSTRAINT", "DEFAULT ACL", "SCHEMA", "TABLE", "SEQUENCE", "INDEX", "CONSTRAINT", "VIEW", "FUNCTION", "EXTENSION", "COMMENT", "TRIGGER", "TYPE", "DEFAULT", "ACL", "DOMAIN", "PROCEDURE", "AGGREGATE"}

// ParseTOC parses `pg_restore --list` output.
func ParseTOC(r io.Reader) []TOCEntry {
	var entries []TOCEntry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		m := tocRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rest := m[1]
		for _, typ := range knownTypes {
			if strings.HasPrefix(rest, typ+" ") {
				fields := strings.Fields(strings.TrimPrefix(rest, typ+" "))
				e := TOCEntry{Type: typ}
				if len(fields) >= 2 {
					e.Schema, e.Name = fields[0], fields[1]
				}
				entries = append(entries, e)
				break
			}
		}
	}
	return entries
}

// CountTables returns the number of ordinary tables in a TOC.
func CountTables(entries []TOCEntry) int {
	n := 0
	for _, e := range entries {
		if e.Type == "TABLE" {
			n++
		}
	}
	return n
}

func start(cmd *exec.Cmd, env []string, stdin io.Reader, wantStdout bool) (*Process, io.ReadCloser, error) {
	// Minimal environment: PATH plus libpq settings. The worker's own
	// environment (which holds DBVault secrets) is never inherited.
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.TempDir(), "LC_ALL=C"}, env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Kill the whole process group so no orphaned children remain.
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 10 * time.Second
	stderr := &tailBuffer{max: 32 << 10}
	cmd.Stderr = stderr
	var stdout io.ReadCloser
	if stdin != nil {
		cmd.Stdin = stdin
	}
	if wantStdout {
		var err error
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return nil, nil, err
		}
	} else {
		cmd.Stdout = io.Discard
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start %s: %w", filepath.Base(cmd.Path), err)
	}
	return &Process{cmd: cmd, stderr: stderr}, stdout, nil
}

// Wait waits for the process and returns a concise error including the tail
// of stderr when it fails.
func (p *Process) Wait() error {
	err := p.cmd.Wait()
	if err != nil {
		return errors.New(p.stderr.Summary(err))
	}
	return nil
}

// Stderr returns the captured tail of stderr.
func (p *Process) Stderr() string { return p.stderr.String() }

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if t.buf.Len() > t.max {
		b := t.buf.Bytes()
		keep := append([]byte(nil), b[len(b)-t.max:]...)
		t.buf.Reset()
		t.buf.Write(keep)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.String()
}

// Summary returns the most relevant stderr lines (errors first).
func (t *tailBuffer) Summary(err error) string {
	lines := strings.Split(strings.TrimSpace(t.String()), "\n")
	var picked []string
	for _, l := range lines {
		if strings.Contains(l, "error:") || strings.Contains(l, "FATAL") || strings.Contains(l, "ERROR") {
			picked = append(picked, strings.TrimSpace(l))
		}
	}
	if len(picked) == 0 && len(lines) > 0 && lines[0] != "" {
		picked = lines
	}
	if len(picked) > 5 {
		picked = picked[len(picked)-5:]
	}
	msg := strings.Join(picked, "; ")
	if msg == "" {
		return err.Error()
	}
	if len(msg) > 1500 {
		msg = msg[:1500] + "..."
	}
	return msg
}
