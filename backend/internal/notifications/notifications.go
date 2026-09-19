// Package notifications delivers backup and restore events to email and
// webhooks. Deliveries run as background jobs with retries and are recorded
// so failures are visible in the dashboard.
package notifications

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/encryption"
	"github.com/dbvault/dbvault/backend/internal/jobs"
	"github.com/dbvault/dbvault/backend/internal/validate"
)

// Event types.
const (
	EventBackupSucceeded    = "backup.succeeded"
	EventBackupFailed       = "backup.failed"
	EventRestoreCompleted   = "restore.completed"
	EventRestoreFailed      = "restore.failed"
	EventVerificationPassed = "verification.passed"
	EventVerificationFailed = "verification.failed"
	EventStorageFailed      = "storage.failed"
	EventTest               = "test"
)

// AllEvents lists subscribable events with descriptions.
var AllEvents = []struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description"`
}{
	{EventBackupFailed, "Backup failed", "A scheduled or manual backup did not complete."},
	{EventBackupSucceeded, "Backup succeeded", "A backup completed and its checksum was verified."},
	{EventVerificationFailed, "Verification failed", "A restore test or integrity check failed."},
	{EventVerificationPassed, "Verification passed", "A backup was restored successfully in a sandbox."},
	{EventRestoreCompleted, "Restore completed", "A restore job finished successfully."},
	{EventRestoreFailed, "Restore failed", "A restore job failed."},
	{EventStorageFailed, "Storage failure", "Uploading to or reading from a storage destination failed."},
}

func validEvent(e string) bool {
	for _, x := range AllEvents {
		if x.Type == e {
			return true
		}
	}
	return false
}

// Detail is a labelled value included in messages.
type Detail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Event is the payload delivered to channels (webhook body).
type Event struct {
	Type           string         `json:"event"`
	DeliveryID     string         `json:"delivery_id"`
	OrganizationID string         `json:"organization_id"`
	Severity       string         `json:"severity"` // info | success | warning | error
	Title          string         `json:"title"`
	Message        string         `json:"message"`
	Details        []Detail       `json:"details,omitempty"`
	Resource       map[string]any `json:"resource,omitempty"`
	URL            string         `json:"url,omitempty"`
	OccurredAt     time.Time      `json:"occurred_at"`
}

// ChannelConfig is the non-secret configuration of a channel.
type ChannelConfig struct {
	Recipients []string `json:"recipients,omitempty"` // email
	URLHint    string   `json:"url_hint,omitempty"`   // webhook (scheme://host only)
}

type Channel struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Type      string        `json:"type"`
	Config    ChannelConfig `json:"config"`
	Events    []string      `json:"events"`
	Enabled   bool          `json:"enabled"`
	HasSecret bool          `json:"has_signing_secret"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	// Last delivery summary.
	LastStatus *string    `json:"last_delivery_status"`
	LastAt     *time.Time `json:"last_delivery_at"`
	sealed     *string
	orgID      string
}

type Service struct {
	Pool    *pgxpool.Pool
	Sealer  *encryption.Sealer
	Queue   *jobs.Queue
	AppURL  string
	Senders map[string]Sender
	// EmailConfigured reports whether SMTP is available (shown in the UI).
	EmailConfigured bool
}

func secretsAAD(id string) string { return "notification:" + id + ":secrets" }

const selectChannel = `SELECT n.id, n.organization_id, n.name, n.type, n.config, n.secret_encrypted, n.events, n.enabled, n.created_at, n.updated_at,
	ld.status, ld.created_at
	FROM notifications n
	LEFT JOIN LATERAL (SELECT status, created_at FROM notification_deliveries d WHERE d.notification_id = n.id AND d.event <> 'test'
	                   ORDER BY created_at DESC LIMIT 1) ld ON true`

func scanChannel(row pgx.Row) (Channel, error) {
	var c Channel
	var cfg []byte
	err := row.Scan(&c.ID, &c.orgID, &c.Name, &c.Type, &cfg, &c.sealed, &c.Events, &c.Enabled, &c.CreatedAt, &c.UpdatedAt, &c.LastStatus, &c.LastAt)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(cfg, &c.Config); err != nil {
		return c, err
	}
	c.HasSecret = c.Type == "webhook" && c.sealed != nil
	return c, nil
}

func (s *Service) List(ctx context.Context, orgID string) ([]Channel, error) {
	rows, err := s.Pool.Query(ctx, selectChannel+` WHERE n.organization_id = $1 ORDER BY n.created_at`, orgID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Channel, error) { return scanChannel(r) })
	if out == nil {
		out = []Channel{}
	}
	return out, err
}

func (s *Service) Get(ctx context.Context, orgID, id string) (Channel, error) {
	c, err := scanChannel(s.Pool.QueryRow(ctx, selectChannel+` WHERE n.id = $1 AND n.organization_id = $2`, id, orgID))
	if db.IsNotFound(err) {
		return c, apperr.NotFound("Notification channel")
	}
	return c, err
}

// Input is the create/update payload.
type Input struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Recipients []string `json:"recipients"`
	URL        *string  `json:"url"`
	Events     []string `json:"events"`
	Enabled    *bool    `json:"enabled"`
}

func (in *Input) Validate(creating bool) error {
	in.Name = strings.TrimSpace(in.Name)
	v := validate.New()
	v.Required("name", in.Name)
	v.MaxLen("name", in.Name, 80)
	v.OneOf("type", in.Type, "email", "webhook")
	v.Check(len(in.Events) > 0, "events", "Select at least one event.")
	for _, e := range in.Events {
		v.Check(validEvent(e), "events", "Unknown event "+e+".")
	}
	switch in.Type {
	case "email":
		v.Check(len(in.Recipients) > 0 && len(in.Recipients) <= 20, "recipients", "Add between 1 and 20 recipients.")
		for i, r := range in.Recipients {
			in.Recipients[i] = strings.ToLower(strings.TrimSpace(r))
			v.Email("recipients", in.Recipients[i])
		}
	case "webhook":
		if creating || (in.URL != nil && *in.URL != "") {
			ok := false
			if in.URL != nil {
				u, err := url.Parse(strings.TrimSpace(*in.URL))
				ok = err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && len(*in.URL) <= 2048
			}
			v.Check(ok, "url", "Must be an http(s) URL.")
		}
	}
	return v.Err()
}

func urlHint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func newSigningSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "whsec_" + base64.RawURLEncoding.EncodeToString(b)
}

// Create adds a channel. For webhooks, the signing secret is returned once.
func (s *Service) Create(ctx context.Context, orgID, userID string, in Input) (Channel, string, error) {
	id := uuid.NewString()
	cfg := ChannelConfig{Recipients: in.Recipients}
	var sealed *string
	var signing string
	if in.Type == "webhook" {
		u := strings.TrimSpace(*in.URL)
		cfg = ChannelConfig{URLHint: urlHint(u)}
		signing = newSigningSecret()
		b, _ := json.Marshal(Secrets{URL: u, SigningSecret: signing})
		v, err := s.Sealer.Seal(b, secretsAAD(id))
		if err != nil {
			return Channel{}, "", err
		}
		sealed = &v
	}
	cfgJSON, _ := json.Marshal(cfg)
	enabled := in.Enabled == nil || *in.Enabled
	err := db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO notifications (id, organization_id, name, type, config, secret_encrypted, events, enabled, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, id, orgID, in.Name, in.Type, cfgJSON, sealed, in.Events, enabled, userID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: orgID, Action: audit.NotificationCreated, ResourceType: "notification", ResourceID: id,
			Metadata: map[string]any{"name": in.Name, "type": in.Type, "events": in.Events}})
	})
	if err != nil {
		return Channel{}, "", err
	}
	c, err := s.Get(ctx, orgID, id)
	return c, signing, err
}

// Update edits a channel; an empty webhook URL keeps the stored one.
func (s *Service) Update(ctx context.Context, orgID, id string, in Input) (Channel, error) {
	existing, err := s.Get(ctx, orgID, id)
	if err != nil {
		return Channel{}, err
	}
	if in.Type != existing.Type {
		return Channel{}, apperr.Validation(map[string]string{"type": "The channel type can't be changed."})
	}
	cfg := ChannelConfig{Recipients: in.Recipients}
	var sealed *string
	if in.Type == "webhook" {
		cfg = existing.Config
		cfg.Recipients = nil
		if in.URL != nil && strings.TrimSpace(*in.URL) != "" {
			current, err := s.secrets(existing)
			if err != nil {
				return Channel{}, err
			}
			u := strings.TrimSpace(*in.URL)
			cfg.URLHint = urlHint(u)
			b, _ := json.Marshal(Secrets{URL: u, SigningSecret: current.SigningSecret})
			v, err := s.Sealer.Seal(b, secretsAAD(id))
			if err != nil {
				return Channel{}, err
			}
			sealed = &v
		}
	}
	enabled := existing.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	cfgJSON, _ := json.Marshal(cfg)
	_, err = s.Pool.Exec(ctx, `UPDATE notifications SET name = $3, config = $4, secret_encrypted = COALESCE($5, secret_encrypted), events = $6, enabled = $7
		WHERE id = $1 AND organization_id = $2`, id, orgID, in.Name, cfgJSON, sealed, in.Events, enabled)
	if err != nil {
		return Channel{}, err
	}
	audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: orgID, Action: audit.NotificationUpdated, ResourceType: "notification", ResourceID: id,
		Metadata: map[string]any{"name": in.Name, "enabled": enabled, "events": in.Events}})
	return s.Get(ctx, orgID, id)
}

func (s *Service) Delete(ctx context.Context, orgID, id string) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM notifications WHERE id = $1 AND organization_id = $2`, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Notification channel")
	}
	audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: orgID, Action: audit.NotificationDeleted, ResourceType: "notification", ResourceID: id})
	return nil
}

func (s *Service) secrets(c Channel) (Secrets, error) {
	var sec Secrets
	if c.sealed == nil {
		return sec, nil
	}
	b, err := s.Sealer.Open(*c.sealed, secretsAAD(c.ID))
	if err != nil {
		return sec, err
	}
	err = json.Unmarshal(b, &sec)
	return sec, err
}

// Emit fans an event out to every enabled channel subscribed to it. It
// never fails the caller: notification problems must not fail backups.
func (s *Service) Emit(ctx context.Context, orgID string, ev Event) {
	ev.OrganizationID = orgID
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	rows, err := s.Pool.Query(ctx, `SELECT id FROM notifications WHERE organization_id = $1 AND enabled AND $2 = ANY(events)`, orgID, ev.Type)
	if err != nil {
		slog.Error("load notification channels", "error", err.Error())
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		slog.Error("load notification channels", "error", err.Error())
		return
	}
	for _, chID := range ids {
		if _, err := s.enqueue(ctx, orgID, chID, ev); err != nil {
			slog.Error("enqueue notification", "error", err.Error(), "notification_id", chID)
		}
	}
}

func (s *Service) enqueue(ctx context.Context, orgID, channelID string, ev Event) (string, error) {
	deliveryID := uuid.NewString()
	ev.DeliveryID = deliveryID
	payload, _ := json.Marshal(ev)
	var jobID string
	err := db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		j, err := s.Queue.Create(ctx, tx, jobs.Spec{OrganizationID: orgID, Type: jobs.TypeNotification,
			Payload: map[string]string{"delivery_id": deliveryID}, MaxAttempts: 4})
		if err != nil {
			return err
		}
		jobID = j.ID
		_, err = tx.Exec(ctx, `INSERT INTO notification_deliveries (id, organization_id, notification_id, job_id, event, payload)
			VALUES ($1, $2, $3, $4, $5, $6)`, deliveryID, orgID, channelID, j.ID, ev.Type, payload)
		return err
	})
	if err != nil {
		return "", err
	}
	_ = s.Queue.Push(ctx, jobID)
	return deliveryID, nil
}

// SendTest enqueues a test message to one channel.
func (s *Service) SendTest(ctx context.Context, orgID, id string) (string, error) {
	c, err := s.Get(ctx, orgID, id)
	if err != nil {
		return "", err
	}
	ev := Event{Type: EventTest, Severity: "info", Title: "Test notification from DBVault",
		Message: "If you can read this, the \"" + c.Name + "\" channel is configured correctly.", URL: s.AppURL + "/notifications",
		OrganizationID: orgID, OccurredAt: time.Now().UTC()}
	deliveryID, err := s.enqueue(ctx, orgID, c.ID, ev)
	if err == nil {
		audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: orgID, Action: audit.NotificationTestedSent, ResourceType: "notification", ResourceID: id})
	}
	return deliveryID, err
}

// Delivery is one attempt record shown in the dashboard.
type Delivery struct {
	ID             string     `json:"id"`
	NotificationID string     `json:"notification_id"`
	ChannelName    string     `json:"channel_name"`
	ChannelType    string     `json:"channel_type"`
	Event          string     `json:"event"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	ResponseStatus *int       `json:"response_status"`
	Error          *string    `json:"error"`
	Title          string     `json:"title"`
	DeliveredAt    *time.Time `json:"delivered_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

// DeliveryFilter narrows and pages the delivery history (newest first).
type DeliveryFilter struct {
	NotificationID string
	Event          string
	Status         string
	Before         *time.Time
	Limit          int
}

func (s *Service) Deliveries(ctx context.Context, orgID string, f DeliveryFilter) ([]Delivery, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	q := `SELECT d.id, d.notification_id, n.name, n.type, d.event, d.status, d.attempts, d.response_status, d.error,
		COALESCE(d.payload->>'title', ''), d.delivered_at, d.created_at
		FROM notification_deliveries d JOIN notifications n ON n.id = d.notification_id
		WHERE d.organization_id = $1`
	args := []any{orgID}
	add := func(clause string, v any) {
		args = append(args, v)
		q += fmt.Sprintf(clause, len(args))
	}
	if f.NotificationID != "" {
		add(` AND d.notification_id = $%d`, f.NotificationID)
	}
	if f.Event != "" {
		add(` AND d.event = $%d`, f.Event)
	}
	if f.Status != "" {
		add(` AND d.status = $%d`, f.Status)
	}
	if f.Before != nil {
		add(` AND d.created_at < $%d`, *f.Before)
	}
	args = append(args, f.Limit)
	q += fmt.Sprintf(` ORDER BY d.created_at DESC LIMIT $%d`, len(args))
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Delivery, error) {
		var d Delivery
		err := r.Scan(&d.ID, &d.NotificationID, &d.ChannelName, &d.ChannelType, &d.Event, &d.Status, &d.Attempts, &d.ResponseStatus, &d.Error, &d.Title, &d.DeliveredAt, &d.CreatedAt)
		return d, err
	})
	if out == nil {
		out = []Delivery{}
	}
	return out, err
}

// Deliver runs a notification job (worker side).
func (s *Service) Deliver(ctx context.Context, job jobs.Job, log *jobs.Logger) (any, error) {
	var p struct {
		DeliveryID string `json:"delivery_id"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return nil, err
	}
	var orgID, channelID string
	var payload []byte
	err := s.Pool.QueryRow(ctx, `SELECT organization_id, notification_id, payload FROM notification_deliveries WHERE id = $1`, p.DeliveryID).
		Scan(&orgID, &channelID, &payload)
	if db.IsNotFound(err) {
		return map[string]string{"skipped": "delivery no longer exists"}, nil
	}
	if err != nil {
		return nil, err
	}
	c, err := s.Get(ctx, orgID, channelID)
	if err != nil {
		return map[string]string{"skipped": "channel no longer exists"}, nil
	}
	var ev Event
	if err := json.Unmarshal(payload, &ev); err != nil {
		return nil, err
	}
	sender, ok := s.Senders[c.Type]
	if !ok {
		return nil, fmt.Errorf("no sender registered for channel type %q", c.Type)
	}
	sec, err := s.secrets(c)
	if err != nil {
		return nil, err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, sendErr := sender.Send(sendCtx, ResolvedChannel{Channel: c, Secrets: sec}, ev)

	var statusPtr *int
	if status != 0 {
		statusPtr = &status
	}
	if sendErr != nil {
		final := job.Attempts >= job.MaxAttempts
		deliveryStatus := "pending"
		if final {
			deliveryStatus = "failed"
		}
		msg := sendErr.Error()
		_, _ = s.Pool.Exec(ctx, `UPDATE notification_deliveries SET status = $2, attempts = $3, response_status = $4, error = $5 WHERE id = $1`,
			p.DeliveryID, deliveryStatus, job.Attempts, statusPtr, msg)
		log.Warnf("Delivery via %q failed (attempt %d of %d): %s", c.Name, job.Attempts, job.MaxAttempts, msg)
		return nil, sendErr
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE notification_deliveries SET status = 'delivered', attempts = $2, response_status = $3, error = NULL, delivered_at = now() WHERE id = $1`,
		p.DeliveryID, job.Attempts, statusPtr)
	log.Infof("Delivered %s notification via %q", ev.Type, c.Name)
	return map[string]any{"delivered": true, "channel": c.Name}, nil
}
