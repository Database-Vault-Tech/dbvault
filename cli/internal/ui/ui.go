// Package ui renders CLI output: colors, tables, progress bars, prompts.
package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"
)

var colorEnabled = term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == ""

// DisableColor turns off ANSI colors (--no-color).
func DisableColor() { colorEnabled = false }

func paint(code, s string) string {
	if !colorEnabled {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func Bold(s string) string   { return paint("1", s) }
func Dim(s string) string    { return paint("2", s) }
func Green(s string) string  { return paint("32", s) }
func Red(s string) string    { return paint("31", s) }
func Yellow(s string) string { return paint("33", s) }
func Cyan(s string) string   { return paint("36", s) }

// Status colors a status word consistently with the dashboard.
func Status(s string) string {
	switch s {
	case "completed", "passed", "pass", "delivered", "ok", "healthy":
		return Green(s)
	case "failed", "fail", "critical":
		return Red(s)
	case "running", "verifying", "queued", "pending":
		return Cyan(s)
	case "unavailable", "warning", "unprotected":
		return Yellow(s)
	default:
		return Dim(s)
	}
}

// Table writes aligned columns.
type Table struct {
	w *tabwriter.Writer
}

func NewTable(out io.Writer, headers ...string) *Table {
	t := &Table{w: tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)}
	for i := range headers {
		headers[i] = Dim(strings.ToUpper(headers[i]))
	}
	fmt.Fprintln(t.w, strings.Join(headers, "\t"))
	return t
}

func (t *Table) Row(cols ...string) { fmt.Fprintln(t.w, strings.Join(cols, "\t")) }
func (t *Table) Flush()             { _ = t.w.Flush() }

// Bytes formats a byte count (1024-based).
func Bytes(n int64) string {
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

// Duration formats milliseconds as "2m 14s".
func Duration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Second {
		return fmt.Sprintf("%dms", ms)
	}
	d = d.Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// Ago formats a time relative to now.
func Ago(t *time.Time) string {
	if t == nil {
		return Dim("never")
	}
	d := time.Since(*t)
	future := d < 0
	if future {
		d = -d
	}
	var s string
	switch {
	case d < time.Minute:
		s = "just now"
		return s
	case d < time.Hour:
		s = fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 48*time.Hour:
		s = fmt.Sprintf("%d hours", int(d.Hours()))
	default:
		s = fmt.Sprintf("%d days", int(d.Hours()/24))
	}
	if future {
		return "in " + s
	}
	return s + " ago"
}

// ProgressBar renders "████████░░░░ 62%".
func ProgressBar(pct int, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * width / 100
	return Green(strings.Repeat("█", filled)) + Dim(strings.Repeat("░", width-filled)) + fmt.Sprintf(" %3d%%", pct)
}

// IsTTY reports whether stdout is interactive.
func IsTTY() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// Prompt asks for a line of input with an optional default.
func Prompt(label, def string) string {
	if def != "" {
		fmt.Printf("%s %s: ", label, Dim("("+def+")"))
	} else {
		fmt.Printf("%s: ", label)
	}
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// PromptSecret reads input without echo.
func PromptSecret(label string) (string, error) {
	fmt.Printf("%s: ", label)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		r := bufio.NewReader(os.Stdin)
		line, err := r.ReadString('\n')
		return strings.TrimRight(line, "\r\n"), err
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), err
}
