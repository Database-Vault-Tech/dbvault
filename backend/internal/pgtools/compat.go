package pgtools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
)

// Restoring into a server older than pg_restore needs care: newer pg_restore
// versions emit session settings the old server doesn't know (pg_restore 18
// always sends "SET transaction_timeout = 0", which only exists from
// PostgreSQL 17), and the server rejects the whole restore.
//
// In that case DBVault runs pg_restore in script mode, drops only header SET
// statements for settings the target server doesn't have, and pipes the rest
// into psql with ON_ERROR_STOP. The script is wrapped in BEGIN/COMMIT and the
// COMMIT is only sent after pg_restore exits successfully, so a failure on
// either side still leaves the target unchanged.

// headerLines bounds where settings are filtered: pg_restore writes its
// session SETs before any object, so data further down is never touched.
const headerLines = 200

var setRE = regexp.MustCompile(`^SET ([a-z_][a-z0-9_.]*) = .*;$`)

// Waiter is a running restore (single process or pipeline).
type Waiter interface{ Wait() error }

func (t Tools) startCompatRestore(ctx context.Context, env []string, o RestoreOptions, stdin io.Reader) (Waiter, error) {
	ctx, cancel := context.WithCancel(ctx)
	args := []string{"--no-owner", "--no-privileges", "--file=-"}
	if o.Clean {
		args = append(args, "--clean", "--if-exists")
	}
	restoreCmd := exec.CommandContext(ctx, t.Path("pg_restore"), args...)
	restoreProc, restoreOut, err := start(restoreCmd, env, stdin, true)
	if err != nil {
		cancel()
		return nil, err
	}

	pr, pw := io.Pipe()
	psqlCmd := exec.CommandContext(ctx, t.Path("psql"), "-X", "-q", "-v", "ON_ERROR_STOP=1")
	psqlProc, _, err := start(psqlCmd, env, pr, false)
	if err != nil {
		cancel()
		_ = restoreProc.Wait()
		return nil, err
	}

	p := &compatPipeline{cancel: cancel, psql: psqlProc, pr: pr, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		p.restoreErr = p.filter(restoreOut, pw, o, restoreProc)
	}()
	return p, nil
}

type compatPipeline struct {
	cancel     context.CancelFunc
	psql       *Process
	pr         *io.PipeReader
	done       chan struct{}
	restoreErr error
}

// filter copies pg_restore's script into psql, dropping unsupported header
// settings, and commits only if pg_restore succeeded.
func (p *compatPipeline) filter(src io.Reader, pw *io.PipeWriter, o RestoreOptions, restore *Process) error {
	fail := func(err error) error {
		// Closing without COMMIT makes psql disconnect with the transaction
		// open, so the server rolls everything back.
		_ = pw.CloseWithError(err)
		p.cancel()
		_ = restore.Wait()
		return err
	}
	w := bufio.NewWriterSize(pw, 256<<10)
	if o.SingleTransaction {
		if _, err := w.WriteString("BEGIN;\n"); err != nil {
			return fail(err)
		}
	}
	r := bufio.NewReaderSize(src, 256<<10)
	for line := 0; ; line++ {
		s, err := r.ReadString('\n')
		if s != "" {
			s = adaptLine(s, line, o.ServerSettings)
			if _, werr := w.WriteString(s); werr != nil {
				return fail(werr)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fail(err)
		}
	}
	if err := restore.Wait(); err != nil {
		return fail(fmt.Errorf("pg_restore failed: %w", err))
	}
	if o.SingleTransaction {
		if _, err := w.WriteString("COMMIT;\n"); err != nil {
			return fail(err)
		}
	}
	if err := w.Flush(); err != nil {
		return fail(err)
	}
	return pw.Close()
}

// adaptLine comments out a header SET for a setting the server doesn't have.
func adaptLine(s string, lineNo int, settings map[string]bool) string {
	if lineNo >= headerLines {
		return s
	}
	if m := setRE.FindStringSubmatch(strings.TrimRight(s, "\r\n")); m != nil && !settings[m[1]] {
		return "-- DBVault: skipped (not supported by the target server): " + s
	}
	return s
}

func (p *compatPipeline) Wait() error {
	psqlErr := p.psql.Wait()
	// Unblock the filter if psql stopped early (e.g. an SQL error).
	_ = p.pr.CloseWithError(io.ErrClosedPipe)
	if psqlErr != nil {
		p.cancel()
	}
	<-p.done
	p.cancel()
	if psqlErr != nil {
		return psqlErr
	}
	return p.restoreErr
}
