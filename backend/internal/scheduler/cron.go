// Package scheduler owns backup schedules and the process that turns due
// schedules into backup jobs. It runs server-side: nothing depends on a
// browser being open.
package scheduler

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Presets map friendly schedule names to cron expressions.
var Presets = map[string]string{
	"hourly":        "0 * * * *",
	"every_6_hours": "0 */6 * * *",
	"daily":         "0 2 * * *",
	"weekly":        "0 3 * * 0",
}

// MinInterval is the shortest allowed gap between two runs.
const MinInterval = 5 * time.Minute

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Resolve returns the cron expression for a preset (or the custom expression).
func Resolve(preset, custom string) (string, error) {
	if preset == "custom" {
		custom = strings.TrimSpace(custom)
		if custom == "" {
			return "", errors.New("enter a cron expression")
		}
		return custom, nil
	}
	expr, ok := Presets[preset]
	if !ok {
		return "", fmt.Errorf("unknown schedule preset %q", preset)
	}
	return expr, nil
}

// Parse validates a cron expression and timezone.
func Parse(expr, tz string) (cron.Schedule, *time.Location, error) {
	if strings.HasPrefix(strings.TrimSpace(expr), "@every") {
		return nil, nil, errors.New("@every is not supported; use a standard 5-field cron expression")
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, nil, fmt.Errorf("unknown timezone %q", tz)
	}
	sched, err := parser.Parse(expr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid cron expression: %v", err)
	}
	return sched, loc, nil
}

// NextRuns returns the next n run times after from.
func NextRuns(sched cron.Schedule, loc *time.Location, from time.Time, n int) []time.Time {
	out := make([]time.Time, 0, n)
	t := from.In(loc)
	for i := 0; i < n; i++ {
		t = sched.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t.UTC())
	}
	return out
}

// Validate checks an expression, timezone and minimum interval.
func Validate(expr, tz string) error {
	sched, loc, err := Parse(expr, tz)
	if err != nil {
		return err
	}
	runs := NextRuns(sched, loc, time.Now(), 12)
	if len(runs) < 2 {
		return errors.New("this expression never runs")
	}
	for i := 1; i < len(runs); i++ {
		if runs[i].Sub(runs[i-1]) < MinInterval {
			return fmt.Errorf("backups may run at most every %s", MinInterval)
		}
	}
	return nil
}

// Describe returns a human-readable summary of a schedule.
func Describe(preset, expr, tz string) string {
	switch preset {
	case "hourly":
		return "Every hour"
	case "every_6_hours":
		return "Every 6 hours"
	case "daily":
		return "Daily at 02:00 " + tz
	case "weekly":
		return "Weekly on Sunday at 03:00 " + tz
	}
	return "Cron: " + expr + " (" + tz + ")"
}
