// Package proc runs external database client programs (pg_dump,
// mariadb-dump, ...) with a minimal environment, kills their whole process
// group on cancellation and keeps the tail of stderr for error messages.
package proc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Process is a running client program.
type Process struct {
	cmd    *exec.Cmd
	stderr *TailBuffer
}

// NewTailBuffer returns a writer that keeps the last max bytes.
func NewTailBuffer(max int) *TailBuffer { return &TailBuffer{max: max} }

func Start(cmd *exec.Cmd, env []string, stdin io.Reader, wantStdout bool) (*Process, io.ReadCloser, error) {
	// Minimal environment: PATH plus the tool's own settings. The worker's own
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
	stderr := NewTailBuffer(32 << 10)
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

// TailBuffer keeps the last max bytes written to it.
type TailBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (t *TailBuffer) Write(p []byte) (int, error) {
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

func (t *TailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.String()
}

// Summary returns the most relevant stderr lines (errors first).
func (t *TailBuffer) Summary(err error) string {
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
