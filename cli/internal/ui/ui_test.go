package ui

import (
	"strings"
	"testing"
)

func TestFormatting(t *testing.T) {
	DisableColor()
	cases := map[int64]string{0: "0 B", 1536: "1.5 KB", 505413632: "482.0 MB"}
	for in, want := range cases {
		if got := Bytes(in); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", in, got, want)
		}
	}
	durations := map[int64]string{500: "500ms", 41000: "41s", 134000: "2m 14s", 3780000: "1h 03m"}
	for in, want := range durations {
		if got := Duration(in); got != want {
			t.Errorf("Duration(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	DisableColor()
	full := ProgressBar(100, 20)
	if !strings.HasPrefix(full, strings.Repeat("█", 20)) || !strings.HasSuffix(full, "100%") {
		t.Fatalf("unexpected full bar %q", full)
	}
	half := ProgressBar(50, 20)
	if strings.Count(half, "█") != 10 || strings.Count(half, "░") != 10 {
		t.Fatalf("unexpected half bar %q", half)
	}
	if ProgressBar(150, 10) != ProgressBar(100, 10) || ProgressBar(-5, 10) != ProgressBar(0, 10) {
		t.Fatal("progress should clamp to 0..100")
	}
}
