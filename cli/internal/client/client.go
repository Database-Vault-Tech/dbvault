// Package client is a small typed client for the DBVault REST API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	base  string
	token string
	org   string
	http  *http.Client
}

func New(server, token, org string) *Client {
	return &Client{base: strings.TrimRight(server, "/"), token: token, org: org, http: &http.Client{Timeout: 60 * time.Second}}
}

// Error is an API error response.
type Error struct {
	Status    int
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields"`
	RequestID string            `json:"request_id"`
}

func (e *Error) Error() string {
	msg := e.Message
	for f, m := range e.Fields {
		msg += fmt.Sprintf("\n  %s: %s", f, m)
	}
	return msg
}

// Do performs a request; out receives the "data" field.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "dbvault-cli")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.org != "" {
		req.Header.Set("X-DBVault-Org", c.org)
	}
	res, err := c.http.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return fmt.Errorf("cannot reach DBVault at %s (%v)", c.base, ue.Err)
		}
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 400 {
		var env struct {
			Error Error `json:"error"`
		}
		if json.Unmarshal(raw, &env) == nil && env.Error.Message != "" {
			env.Error.Status = res.StatusCode
			return &env.Error
		}
		return &Error{Status: res.StatusCode, Message: fmt.Sprintf("unexpected HTTP %d from server", res.StatusCode)}
	}
	if out == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("unexpected response from server: %w", err)
	}
	return json.Unmarshal(env.Data, out)
}

func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	if body == nil {
		body = map[string]any{}
	}
	return c.Do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) Put(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPut, path, body, out)
}

func (c *Client) Delete(ctx context.Context, path string) error {
	return c.Do(ctx, http.MethodDelete, path, nil, nil)
}

// ---- API types (subset of the server's JSON) ----

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role"`
}

type Me struct {
	User          User           `json:"user"`
	Organizations []Organization `json:"organizations"`
}

type Database struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Host             string     `json:"host"`
	Port             int        `json:"port"`
	Database         string     `json:"database"`
	Username         string     `json:"username"`
	SSLMode          string     `json:"ssl_mode"`
	Engine           string     `json:"engine"`
	PGVersion        *string    `json:"pg_version"`
	Protected        bool       `json:"protected"`
	LastBackupAt     *time.Time `json:"last_backup_at"`
	LastBackupStatus *string    `json:"last_backup_status"`
	NextRunAt        *time.Time `json:"next_run_at"`
	StorageBytes     int64      `json:"storage_bytes"`
}

type ServerInfo struct {
	Version    string `json:"version"`
	Major      int    `json:"major"`
	SizeBytes  int64  `json:"size_bytes"`
	TableCount int    `json:"table_count"`
	LatencyMS  int64  `json:"latency_ms"`
}

type ConnectionTest struct {
	OK      bool        `json:"ok"`
	Message string      `json:"message"`
	Server  *ServerInfo `json:"server"`
}

type Storage struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	Location    string     `json:"location"`
	IsDefault   bool       `json:"is_default"`
	BackupCount int        `json:"backup_count"`
	UsedBytes   int64      `json:"used_bytes"`
	LastTestOK  *bool      `json:"last_test_ok"`
	LastTested  *time.Time `json:"last_tested_at"`
}

type Retention struct {
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
}

type Schedule struct {
	ID               string     `json:"id"`
	DatabaseName     string     `json:"database_name"`
	StorageName      string     `json:"storage_name"`
	Description      string     `json:"description"`
	CronExpression   string     `json:"cron_expression"`
	Timezone         string     `json:"timezone"`
	Enabled          bool       `json:"enabled"`
	Retention        Retention  `json:"retention"`
	NextRunAt        *time.Time `json:"next_run_at"`
	LastBackupStatus *string    `json:"last_backup_status"`
}

type Check struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type Verification struct {
	Integrity      Check  `json:"integrity"`
	Restore        Check  `json:"restore"`
	Database       Check  `json:"database"`
	TablesExpected int    `json:"tables_expected"`
	TablesRestored int    `json:"tables_restored"`
	Rows           int64  `json:"rows"`
	Sandbox        string `json:"sandbox"`
	DurationMS     int64  `json:"duration_ms"`
}

type Backup struct {
	ID                 string        `json:"id"`
	DatabaseID         string        `json:"database_id"`
	DatabaseName       string        `json:"database_name"`
	StorageName        string        `json:"storage_name"`
	StorageType        string        `json:"storage_type"`
	JobID              *string       `json:"job_id"`
	Trigger            string        `json:"trigger"`
	Status             string        `json:"status"`
	StorageKey         *string       `json:"storage_key"`
	Compression        string        `json:"compression"`
	Encrypted          bool          `json:"encrypted"`
	SizeBytes          *int64        `json:"size_bytes"`
	RawSizeBytes       *int64        `json:"raw_size_bytes"`
	Checksum           *string       `json:"checksum_sha256"`
	PGVersion          *string       `json:"pg_version"`
	Error              *string       `json:"error"`
	DurationMS         *int64        `json:"duration_ms"`
	VerificationStatus string        `json:"verification_status"`
	Verification       *Verification `json:"verification"`
	CreatedAt          time.Time     `json:"created_at"`
}

type Progress struct {
	Phase          string `json:"phase"`
	BytesDumped    int64  `json:"bytes_dumped"`
	BytesWritten   int64  `json:"bytes_written"`
	EstimatedTotal int64  `json:"estimated_total"`
}

type Job struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Status   string    `json:"status"`
	Error    *string   `json:"error"`
	Progress *Progress `json:"progress"`
}

type LogEntry struct {
	ID        int64     `json:"id"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type Queued struct {
	BackupID string `json:"backup_id"`
	JobID    string `json:"job_id"`
	Status   string `json:"status"`
}

type Restore struct {
	ID                 string  `json:"id"`
	JobID              *string `json:"job_id"`
	Status             string  `json:"status"`
	Error              *string `json:"error"`
	TargetDatabaseName string  `json:"target_database_name"`
	Mode               string  `json:"mode"`
	NewDatabaseName    *string `json:"new_database_name"`
	DurationMS         *int64  `json:"duration_ms"`
	MaskingProfile     *string `json:"masking_profile"`
	MaskingReport      *struct {
		RowsChanged int64 `json:"rows_changed"`
		Checks      int   `json:"checks_passed"`
	} `json:"masking_report"`
}

type SystemStatus struct {
	Version       string `json:"version"`
	WorkersOnline int    `json:"workers_online"`
	Verification  struct {
		Available bool   `json:"available"`
		Mode      string `json:"mode"`
		Message   string `json:"message"`
	} `json:"verification"`
	EmailConfigured bool `json:"email_configured"`
}

type Stats struct {
	Databases            int        `json:"databases"`
	ProtectedDatabases   int        `json:"protected_databases"`
	BackupsToday         int        `json:"backups_today"`
	StorageUsedBytes     int64      `json:"storage_used_bytes"`
	FailedBackups7d      int        `json:"failed_backups_7d"`
	LastSuccessfulBackup *time.Time `json:"last_successful_backup"`
	RunningJobs          int        `json:"running_jobs"`
}
