package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dbvault/dbvault/cli/internal/client"
	"github.com/dbvault/dbvault/cli/internal/ui"
)

type jobView struct {
	Job  client.Job        `json:"job"`
	Logs []client.LogEntry `json:"logs"`
}

// followJob polls a job until it finishes. With verbose, log lines are
// streamed; otherwise a progress bar is drawn for backups.
func followJob(ctx context.Context, c *client.Client, jobID string, verbose, bar bool) (client.Job, error) {
	var lastLog int64
	drawn := false
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		var v jobView
		if err := c.Get(ctx, "/jobs/"+jobID, &v); err != nil {
			return client.Job{}, err
		}
		if verbose {
			for _, l := range v.Logs {
				if l.ID <= lastLog {
					continue
				}
				lastLog = l.ID
				msg := l.Message
				switch l.Level {
				case "warn":
					msg = ui.Yellow(msg)
				case "error":
					msg = ui.Red(msg)
				}
				fmt.Printf("%s %s\n", ui.Dim(l.CreatedAt.Local().Format("15:04:05")), msg)
			}
		} else if bar && ui.IsTTY() {
			pct := 0
			label := ""
			if p := v.Job.Progress; p != nil {
				if p.EstimatedTotal > 0 {
					pct = int(p.BytesDumped * 100 / p.EstimatedTotal)
				}
				if pct > 99 {
					pct = 99
				}
				label = ui.Dim(fmt.Sprintf("  %s dumped", ui.Bytes(p.BytesDumped)))
				if p.Phase == "verifying" {
					pct, label = 99, ui.Dim("  verifying checksum")
				}
			}
			if v.Job.Status == "completed" {
				pct, label = 100, ""
			}
			fmt.Printf("\r%s%s\x1b[K", ui.ProgressBar(pct, 20), label)
			drawn = true
		}
		switch v.Job.Status {
		case "completed", "failed", "cancelled":
			if drawn {
				fmt.Println()
			}
			return v.Job, nil
		}
		select {
		case <-ctx.Done():
			if drawn {
				fmt.Println()
			}
			return v.Job, fmt.Errorf("stopped following (the job keeps running on the server; cancel it with the dashboard)")
		case <-tick.C:
		}
	}
}

func jobError(j client.Job) string {
	if j.Error != nil {
		return strings.TrimSpace(*j.Error)
	}
	return j.Status
}
