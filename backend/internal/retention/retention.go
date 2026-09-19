// Package retention decides which backups a policy keeps.
//
// DBVault uses a grandfather-father-son (GFS) policy: keep the newest
// backup of each of the last N days, N ISO weeks and N months that have
// backups. A backup is kept if any rule keeps it. Planning is a pure
// function so it is easy to test and to preview in the UI before anything
// is deleted.
package retention

import (
	"fmt"
	"sort"
	"time"
)

type Policy struct {
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
}

// Enabled reports whether the policy deletes anything at all. A policy of
// all zeros means "keep everything".
func (p Policy) Enabled() bool { return p.Daily > 0 || p.Weekly > 0 || p.Monthly > 0 }

func (p Policy) String() string {
	if !p.Enabled() {
		return "keep all backups"
	}
	return fmt.Sprintf("daily %d, weekly %d, monthly %d", p.Daily, p.Weekly, p.Monthly)
}

// Item is a completed backup eligible for retention.
type Item struct {
	ID        string
	CreatedAt time.Time
}

// Decision explains what happens to one backup.
type Decision struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Keep      bool      `json:"keep"`
	Reasons   []string  `json:"reasons"`
}

// MinAge protects very recent backups from deletion regardless of policy.
const MinAge = time.Hour

// Plan returns one decision per item, newest first. loc determines calendar
// boundaries (the schedule's timezone).
func Plan(items []Item, p Policy, now time.Time, loc *time.Location) []Decision {
	if loc == nil {
		loc = time.UTC
	}
	sorted := append([]Item(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].CreatedAt.After(sorted[j].CreatedAt) })

	decisions := make([]Decision, len(sorted))
	for i, it := range sorted {
		decisions[i] = Decision{ID: it.ID, CreatedAt: it.CreatedAt}
	}
	if len(sorted) == 0 {
		return decisions
	}
	keep := func(i int, reason string) {
		decisions[i].Keep = true
		decisions[i].Reasons = append(decisions[i].Reasons, reason)
	}

	if !p.Enabled() {
		for i := range decisions {
			keep(i, "retention disabled")
		}
		return decisions
	}

	// The newest backup is always kept, whatever the policy says.
	keep(0, "latest backup")

	bucket := func(limit int, label string, key func(time.Time) string) {
		if limit <= 0 {
			return
		}
		seen := map[string]bool{}
		for i, it := range sorted {
			k := key(it.CreatedAt.In(loc))
			if seen[k] {
				continue
			}
			if len(seen) >= limit {
				return
			}
			seen[k] = true
			keep(i, label+" "+k)
		}
	}
	bucket(p.Daily, "daily", func(t time.Time) string { return t.Format("2006-01-02") })
	bucket(p.Weekly, "weekly", func(t time.Time) string {
		y, w := t.ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	})
	bucket(p.Monthly, "monthly", func(t time.Time) string { return t.Format("2006-01") })

	for i, it := range sorted {
		if !decisions[i].Keep && now.Sub(it.CreatedAt) < MinAge {
			keep(i, "younger than 1 hour")
		}
		if !decisions[i].Keep {
			decisions[i].Reasons = []string{"outside retention policy (" + p.String() + ")"}
		}
	}
	return decisions
}
