package scheduler

import (
	"testing"
	"time"
)

func TestResolvePresets(t *testing.T) {
	for preset, want := range Presets {
		got, err := Resolve(preset, "")
		if err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v", preset, got, err)
		}
	}
	if _, err := Resolve("custom", " "); err == nil {
		t.Error("empty custom expression must fail")
	}
	if _, err := Resolve("fortnightly", ""); err == nil {
		t.Error("unknown preset must fail")
	}
}

func TestValidate(t *testing.T) {
	valid := []string{"0 * * * *", "*/15 * * * *", "30 2 * * 1-5", "@daily", "@hourly"}
	for _, e := range valid {
		if err := Validate(e, "UTC"); err != nil {
			t.Errorf("%q should be valid: %v", e, err)
		}
	}
	invalid := []string{"* * * * *", "*/2 * * * *", "61 * * * *", "not cron", "@every 1h", "0 0 31 2 *"}
	for _, e := range invalid {
		if err := Validate(e, "UTC"); err == nil {
			t.Errorf("%q should be invalid", e)
		}
	}
	if err := Validate("0 * * * *", "Mars/Olympus"); err == nil {
		t.Error("unknown timezone must fail")
	}
}

func TestNextRunsRespectTimezone(t *testing.T) {
	sched, loc, err := Parse("0 2 * * *", "Africa/Nairobi")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	from := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC) // 03:00 in Nairobi
	runs := NextRuns(sched, loc, from, 2)
	want := time.Date(2026, 9, 19, 23, 0, 0, 0, time.UTC) // 02:00 next day Nairobi (UTC+3)
	if !runs[0].Equal(want) {
		t.Fatalf("next run %s, want %s", runs[0], want)
	}
	if runs[1].Sub(runs[0]) != 24*time.Hour {
		t.Fatal("daily runs should be 24h apart")
	}
}

func TestDescribe(t *testing.T) {
	if Describe("every_6_hours", "0 */6 * * *", "UTC") != "Every 6 hours" {
		t.Error("unexpected description")
	}
	if Describe("custom", "15 3 * * *", "UTC") != "Cron: 15 3 * * * (UTC)" {
		t.Error("unexpected custom description")
	}
}
