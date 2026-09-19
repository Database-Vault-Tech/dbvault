package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LogEntry is one line of a job's user-visible log.
type LogEntry struct {
	ID        int64     `json:"id"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// Logger writes job logs to the database (for the dashboard) and to the
// structured process log (for operators).
type Logger struct {
	pool  *pgxpool.Pool
	jobID string
	log   *slog.Logger
}

func NewLogger(pool *pgxpool.Pool, jobID string, base *slog.Logger) *Logger {
	return &Logger{pool: pool, jobID: jobID, log: base}
}

// Slog returns the structured logger enriched with job context.
func (l *Logger) Slog() *slog.Logger { return l.log }

func (l *Logger) write(level, msg string) {
	// Logging must never fail a job; use a detached context so lines are
	// still written while a cancelled job winds down.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := l.pool.Exec(ctx, `INSERT INTO job_logs (job_id, level, message) VALUES ($1, $2, $3)`, l.jobID, level, msg); err != nil {
		l.log.Warn("failed to persist job log", "error", err.Error())
	}
}

func (l *Logger) Infof(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.log.Info(msg)
	l.write("info", msg)
}

func (l *Logger) Warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.log.Warn(msg)
	l.write("warn", msg)
}

func (l *Logger) Errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.log.Error(msg)
	l.write("error", msg)
}

// Logs returns log lines for a job after the given line id.
func Logs(ctx context.Context, pool *pgxpool.Pool, jobID string, afterID int64) ([]LogEntry, error) {
	rows, err := pool.Query(ctx, `SELECT id, level, message, created_at FROM job_logs WHERE job_id = $1 AND id > $2 ORDER BY id LIMIT 2000`, jobID, afterID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (LogEntry, error) {
		var e LogEntry
		err := r.Scan(&e.ID, &e.Level, &e.Message, &e.CreatedAt)
		return e, err
	})
	if out == nil {
		out = []LogEntry{}
	}
	return out, err
}
