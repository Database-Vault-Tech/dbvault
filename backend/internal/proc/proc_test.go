package proc

import (
	"errors"
	"strings"
	"testing"
)

func TestTailBufferSummary(t *testing.T) {
	tb := NewTailBuffer(64)
	_, _ = tb.Write([]byte(strings.Repeat("x", 100)))
	if len(tb.String()) != 64 {
		t.Fatalf("tail buffer kept %d bytes", len(tb.String()))
	}
	tb2 := NewTailBuffer(4096)
	_, _ = tb2.Write([]byte("pg_dump: connecting\npg_dump: error: query failed: permission denied for table secrets\n"))
	s := tb2.Summary(errors.New("exit status 1"))
	if !strings.Contains(s, "permission denied") || strings.Contains(s, "connecting") {
		t.Fatalf("summary should keep the error line only: %q", s)
	}
	if NewTailBuffer(10).Summary(errors.New("exit status 2")) != "exit status 2" {
		t.Fatal("empty stderr should fall back to the exit error")
	}
}
