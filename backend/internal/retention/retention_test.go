package retention

import (
	"fmt"
	"testing"
	"time"
)

func mk(n int, start time.Time, step time.Duration) []Item {
	items := make([]Item, n)
	for i := range items {
		items[i] = Item{ID: fmt.Sprintf("b%03d", i), CreatedAt: start.Add(-time.Duration(i) * step)}
	}
	return items
}

func kept(ds []Decision) map[string]bool {
	out := map[string]bool{}
	for _, d := range ds {
		if d.Keep {
			out[d.ID] = true
		}
	}
	return out
}

func TestPlanDisabledKeepsEverything(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	ds := Plan(mk(50, now, 6*time.Hour), Policy{}, now, time.UTC)
	for _, d := range ds {
		if !d.Keep {
			t.Fatalf("backup %s deleted with retention disabled", d.ID)
		}
	}
}

func TestPlanDailyKeepsNewestPerDay(t *testing.T) {
	now := time.Date(2026, 9, 19, 23, 0, 0, 0, time.UTC)
	// 4 backups per day for 10 days, newest at 22:00.
	items := mk(40, now.Add(-time.Hour), 6*time.Hour)
	ds := Plan(items, Policy{Daily: 7}, now, time.UTC)
	k := kept(ds)
	if len(k) != 7 {
		t.Fatalf("expected 7 kept, got %d", len(k))
	}
	// Newest of each day is index 0,4,8,... (22:00 each day).
	for day := 0; day < 7; day++ {
		id := fmt.Sprintf("b%03d", day*4)
		if !k[id] {
			t.Errorf("expected %s (newest of day %d) to be kept", id, day)
		}
	}
	if k["b001"] {
		t.Errorf("older backup on the same day should be deleted")
	}
}

func TestPlanGFSCombination(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	// One backup per day for 400 days.
	items := mk(400, now.Add(-2*time.Hour), 24*time.Hour)
	ds := Plan(items, Policy{Daily: 7, Weekly: 4, Monthly: 6}, now, time.UTC)
	k := kept(ds)
	// 7 daily + up to 4 weekly + up to 6 monthly, with overlaps; must be
	// bounded and include the 7 most recent days.
	if len(k) < 7 || len(k) > 17 {
		t.Fatalf("unexpected kept count %d", len(k))
	}
	for i := 0; i < 7; i++ {
		if !k[fmt.Sprintf("b%03d", i)] {
			t.Errorf("recent daily backup b%03d must be kept", i)
		}
	}
	// Anything older than ~6 months must be gone.
	for _, d := range ds {
		if d.Keep && now.Sub(d.CreatedAt) > 200*24*time.Hour {
			t.Errorf("backup %s from %s should have expired", d.ID, d.CreatedAt)
		}
	}
}

func TestPlanAlwaysKeepsLatest(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	items := []Item{{ID: "old", CreatedAt: now.Add(-900 * 24 * time.Hour)}}
	ds := Plan(items, Policy{Daily: 1}, now, time.UTC)
	if !ds[0].Keep {
		t.Fatal("the only (latest) backup must never be deleted")
	}
}

func TestPlanProtectsRecentBackups(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	items := []Item{
		{ID: "a", CreatedAt: now.Add(-10 * time.Minute)},
		{ID: "b", CreatedAt: now.Add(-20 * time.Minute)},
		{ID: "c", CreatedAt: now.Add(-3 * time.Hour)},
	}
	k := kept(Plan(items, Policy{Daily: 1}, now, time.UTC))
	if !k["a"] || !k["b"] {
		t.Fatalf("backups younger than MinAge must be kept: %v", k)
	}
	if k["c"] {
		t.Fatalf("older same-day backup should be deleted: %v", k)
	}
}

func TestPlanRespectsTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	// 03:00 UTC and 02:00 UTC on Sep 19 are Sep 18 evening in New York;
	// 13:00 UTC Sep 18 is the same NY day as well.
	items := []Item{
		{ID: "x", CreatedAt: time.Date(2026, 9, 19, 3, 0, 0, 0, time.UTC)},
		{ID: "y", CreatedAt: time.Date(2026, 9, 19, 2, 0, 0, 0, time.UTC)},
		{ID: "z", CreatedAt: time.Date(2026, 9, 18, 13, 0, 0, 0, time.UTC)},
	}
	k := kept(Plan(items, Policy{Daily: 1}, now, loc))
	if len(k) != 1 || !k["x"] {
		t.Fatalf("expected only x kept in New York calendar, got %v", k)
	}
}

func TestPlanUnsortedInput(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	items := []Item{
		{ID: "older", CreatedAt: now.Add(-50 * time.Hour)},
		{ID: "newest", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "middle", CreatedAt: now.Add(-26 * time.Hour)},
	}
	ds := Plan(items, Policy{Daily: 2}, now, time.UTC)
	if ds[0].ID != "newest" {
		t.Fatalf("decisions must be newest first, got %s", ds[0].ID)
	}
	k := kept(ds)
	if !k["newest"] || !k["middle"] || k["older"] {
		t.Fatalf("unexpected decisions %v", k)
	}
}
