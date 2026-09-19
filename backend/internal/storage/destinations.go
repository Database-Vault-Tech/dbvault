package storage

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/config"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/encryption"
	"github.com/dbvault/dbvault/backend/internal/validate"
)

// Destination is a configured storage location as exposed by the API.
// Secret keys are never returned; only a masked access key hint.
type Destination struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	Config         Config     `json:"config"`
	AccessKeyHint  string     `json:"access_key_hint,omitempty"`
	IsDefault      bool       `json:"is_default"`
	LastTestedAt   *time.Time `json:"last_tested_at"`
	LastTestOK     *bool      `json:"last_test_ok"`
	LastTestError  *string    `json:"last_test_error"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	BackupCount    int        `json:"backup_count"`
	UsedBytes      int64      `json:"used_bytes"`
	ScheduleCount  int        `json:"schedule_count"`
	Location       string     `json:"location"`
	organizationID string
	sealedCreds    *string
}

type DestinationService struct {
	Pool    *pgxpool.Pool
	Sealer  *encryption.Sealer
	Options Options
	Builtin config.S3Defaults
}

func credsAAD(id string) string { return "storage:" + id + ":credentials" }

const selectDestination = `SELECT d.id, d.organization_id, d.name, d.type, d.config, d.credentials_encrypted, d.is_default,
	d.last_tested_at, d.last_test_ok, d.last_test_error, d.created_at, d.updated_at,
	(SELECT count(*) FROM backups b WHERE b.storage_destination_id = d.id AND b.status = 'completed'),
	COALESCE((SELECT sum(b.size_bytes) FROM backups b WHERE b.storage_destination_id = d.id AND b.status = 'completed'), 0)::bigint,
	(SELECT count(*) FROM backup_schedules s WHERE s.storage_destination_id = d.id)
	FROM storage_destinations d`

func (s *DestinationService) scan(row pgx.Row) (Destination, error) {
	var d Destination
	var cfg []byte
	err := row.Scan(&d.ID, &d.organizationID, &d.Name, &d.Type, &cfg, &d.sealedCreds, &d.IsDefault, &d.LastTestedAt, &d.LastTestOK,
		&d.LastTestError, &d.CreatedAt, &d.UpdatedAt, &d.BackupCount, &d.UsedBytes, &d.ScheduleCount)
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal(cfg, &d.Config); err != nil {
		return d, err
	}
	d.Location = describe(d.Type, d.Config)
	if d.sealedCreds != nil {
		if creds, err := s.openCreds(d.ID, *d.sealedCreds); err == nil {
			d.AccessKeyHint = maskKey(creds.AccessKeyID)
		}
	}
	return d, nil
}

func describe(typ string, c Config) string {
	switch typ {
	case TypeLocal:
		return "local:" + "/" + strings.TrimPrefix(c.Path, "/")
	default:
		loc := typ + "://" + c.Bucket
		if c.Prefix != "" {
			loc += "/" + strings.Trim(c.Prefix, "/")
		}
		return loc
	}
}

func maskKey(k string) string {
	if len(k) <= 8 {
		return strings.Repeat("•", len(k))
	}
	return k[:4] + "••••" + k[len(k)-4:]
}

func (s *DestinationService) openCreds(id, sealed string) (Credentials, error) {
	var c Credentials
	b, err := s.Sealer.Open(sealed, credsAAD(id))
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	return c, err
}

func (s *DestinationService) List(ctx context.Context, orgID string) ([]Destination, error) {
	rows, err := s.Pool.Query(ctx, selectDestination+` WHERE d.organization_id = $1 AND d.deleted_at IS NULL ORDER BY d.is_default DESC, lower(d.name)`, orgID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Destination, error) { return s.scan(r) })
	if out == nil {
		out = []Destination{}
	}
	return out, err
}

func (s *DestinationService) Get(ctx context.Context, orgID, id string) (Destination, error) {
	d, err := s.scan(s.Pool.QueryRow(ctx, selectDestination+` WHERE d.id = $1 AND d.organization_id = $2 AND d.deleted_at IS NULL`, id, orgID))
	if db.IsNotFound(err) {
		return d, apperr.NotFound("Storage destination")
	}
	return d, err
}

// GetIncludingDeleted is used when reading existing backups whose
// destination may have been removed from the UI.
func (s *DestinationService) GetIncludingDeleted(ctx context.Context, orgID, id string) (Destination, error) {
	d, err := s.scan(s.Pool.QueryRow(ctx, selectDestination+` WHERE d.id = $1 AND d.organization_id = $2`, id, orgID))
	if db.IsNotFound(err) {
		return d, apperr.NotFound("Storage destination")
	}
	return d, err
}

// Default returns the organization's default destination, if any.
func (s *DestinationService) Default(ctx context.Context, orgID string) (Destination, error) {
	d, err := s.scan(s.Pool.QueryRow(ctx, selectDestination+` WHERE d.organization_id = $1 AND d.deleted_at IS NULL
		ORDER BY d.is_default DESC, d.created_at LIMIT 1`, orgID))
	if db.IsNotFound(err) {
		return d, apperr.Unprocessable("no_storage", "No storage destination is configured. Add one under Storage first.")
	}
	return d, err
}

// Open builds a live Storage for a destination (decrypting credentials).
func (s *DestinationService) Open(ctx context.Context, d Destination) (Storage, error) {
	var creds Credentials
	if d.sealedCreds != nil {
		c, err := s.openCreds(d.ID, *d.sealedCreds)
		if err != nil {
			return nil, err
		}
		creds = c
	}
	return New(ctx, d.Type, d.Config, creds, s.Options)
}

// Input is the create/update payload.
type Input struct {
	Name            string  `json:"name"`
	Type            string  `json:"type"`
	Config          Config  `json:"config"`
	AccessKeyID     *string `json:"access_key_id"`
	SecretAccessKey *string `json:"secret_access_key"`
	IsDefault       bool    `json:"is_default"`
	// UseBuiltin fills endpoint, bucket and credentials from the server's
	// S3_* settings (the MinIO bundled with docker compose).
	UseBuiltin bool `json:"use_builtin"`
}

var (
	bucketRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.\-]{1,61}[a-z0-9]$`)
	pathRE   = regexp.MustCompile(`^[a-zA-Z0-9_\-./]*$`)
	regionRE = regexp.MustCompile(`^[a-z0-9\-]{2,32}$`)
	acctRE   = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

// Normalize validates the input and applies builtin defaults.
func (s *DestinationService) Normalize(in *Input, creating bool) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.UseBuiltin {
		if !s.Builtin.Configured() {
			return apperr.Unprocessable("builtin_unavailable", "Built-in storage is not configured on this server.")
		}
		in.Type = TypeMinIO
		in.Config.Endpoint = s.Builtin.Endpoint
		in.Config.Bucket = s.Builtin.Bucket
		in.Config.Region = s.Builtin.Region
		in.Config.ForcePathStyle = true
		ak, sk := s.Builtin.AccessKey, s.Builtin.SecretKey
		in.AccessKeyID, in.SecretAccessKey = &ak, &sk
	}
	c := &in.Config
	c.Bucket = strings.TrimSpace(c.Bucket)
	c.Endpoint = strings.TrimRight(strings.TrimSpace(c.Endpoint), "/")
	c.Prefix = strings.Trim(strings.TrimSpace(c.Prefix), "/")
	c.Path = strings.Trim(strings.TrimSpace(c.Path), "/")
	c.Region = strings.TrimSpace(c.Region)
	c.AccountID = strings.TrimSpace(c.AccountID)

	v := validate.New()
	v.Required("name", in.Name)
	v.Check(v.Has("name") || validate.IsResourceName(in.Name), "name", "Use letters, numbers, spaces, dots, dashes or underscores (max 63).")
	v.OneOf("type", in.Type, TypeLocal, TypeS3, TypeR2, TypeMinIO)
	v.Check(pathRE.MatchString(c.Prefix) && !strings.Contains(c.Prefix, ".."), "config.prefix", "Use letters, numbers, dashes, underscores, dots and slashes.")
	v.MaxLen("config.prefix", c.Prefix, 200)
	switch in.Type {
	case TypeLocal:
		v.Check(pathRE.MatchString(c.Path) && !strings.Contains(c.Path, ".."), "config.path", "Use a relative directory with letters, numbers, dashes, underscores and slashes.")
		v.MaxLen("config.path", c.Path, 200)
		*c = Config{Path: c.Path, Prefix: c.Prefix}
		in.AccessKeyID, in.SecretAccessKey = nil, nil
	case TypeS3, TypeR2, TypeMinIO:
		v.Check(bucketRE.MatchString(c.Bucket), "config.bucket", "Must be a valid bucket name (3-63 lowercase letters, numbers, dots, dashes).")
		if c.Endpoint != "" {
			u, err := url.Parse(c.Endpoint)
			v.Check(err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil, "config.endpoint", "Must be an http(s) URL such as https://s3.example.com.")
		}
		switch in.Type {
		case TypeS3:
			v.Check(regionRE.MatchString(c.Region), "config.region", "Region is required, e.g. eu-west-1.")
		case TypeR2:
			v.Check(c.Endpoint != "" || acctRE.MatchString(c.AccountID), "config.account_id", "Enter your 32-character Cloudflare account ID (or a custom endpoint).")
			c.Region = "auto"
		case TypeMinIO:
			v.Check(c.Endpoint != "", "config.endpoint", "Endpoint is required for MinIO, e.g. http://minio:9000.")
			c.ForcePathStyle = true
		}
		if creating {
			v.Check(in.AccessKeyID != nil && *in.AccessKeyID != "", "access_key_id", "This field is required.")
			v.Check(in.SecretAccessKey != nil && *in.SecretAccessKey != "", "secret_access_key", "This field is required.")
		}
	}
	return v.Err()
}

func (in Input) credentials() Credentials {
	var c Credentials
	if in.AccessKeyID != nil {
		c.AccessKeyID = strings.TrimSpace(*in.AccessKeyID)
	}
	if in.SecretAccessKey != nil {
		c.SecretAccessKey = strings.TrimSpace(*in.SecretAccessKey)
	}
	return c
}

func (s *DestinationService) sealCreds(id string, c Credentials) (*string, error) {
	if c.AccessKeyID == "" && c.SecretAccessKey == "" {
		return nil, nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	sealed, err := s.Sealer.Seal(b, credsAAD(id))
	return &sealed, err
}

func (s *DestinationService) Create(ctx context.Context, orgID, userID string, in Input) (Destination, error) {
	id := uuid.NewString()
	sealed, err := s.sealCreds(id, in.credentials())
	if err != nil {
		return Destination{}, err
	}
	cfg, _ := json.Marshal(in.Config)
	err = db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var existing int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM storage_destinations WHERE organization_id = $1 AND deleted_at IS NULL`, orgID).Scan(&existing); err != nil {
			return err
		}
		isDefault := in.IsDefault || existing == 0 // the first destination becomes the default
		if isDefault {
			if _, err := tx.Exec(ctx, `UPDATE storage_destinations SET is_default = false WHERE organization_id = $1 AND is_default`, orgID); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO storage_destinations (id, organization_id, name, type, config, credentials_encrypted, is_default, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, orgID, in.Name, in.Type, cfg, sealed, isDefault, userID)
		if db.IsUniqueViolation(err) {
			return apperr.Validation(map[string]string{"name": "A storage destination with this name already exists."})
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: orgID, Action: audit.StorageCreated, ResourceType: "storage", ResourceID: id,
			Metadata: map[string]any{"name": in.Name, "type": in.Type, "location": describe(in.Type, in.Config)}})
	})
	if err != nil {
		return Destination{}, err
	}
	return s.Get(ctx, orgID, id)
}

// Update changes name, config, credentials (when provided) and default flag.
func (s *DestinationService) Update(ctx context.Context, orgID, id string, in Input) (Destination, error) {
	existing, err := s.Get(ctx, orgID, id)
	if err != nil {
		return Destination{}, err
	}
	if in.Type != existing.Type {
		return Destination{}, apperr.Validation(map[string]string{"type": "The storage type can't be changed. Create a new destination instead."})
	}
	var sealed *string
	if c := in.credentials(); c.AccessKeyID != "" || c.SecretAccessKey != "" {
		if c.AccessKeyID == "" || c.SecretAccessKey == "" {
			return Destination{}, apperr.Validation(map[string]string{"secret_access_key": "Provide both the access key and secret key to replace credentials."})
		}
		if sealed, err = s.sealCreds(id, c); err != nil {
			return Destination{}, err
		}
	}
	cfg, _ := json.Marshal(in.Config)
	err = db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		if in.IsDefault {
			if _, err := tx.Exec(ctx, `UPDATE storage_destinations SET is_default = false WHERE organization_id = $1 AND is_default AND id <> $2`, orgID, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE storage_destinations SET name = $3, config = $4, credentials_encrypted = COALESCE($5, credentials_encrypted),
			is_default = $6 OR is_default, last_tested_at = NULL, last_test_ok = NULL, last_test_error = NULL
			WHERE id = $1 AND organization_id = $2`, id, orgID, in.Name, cfg, sealed, in.IsDefault)
		if db.IsUniqueViolation(err) {
			return apperr.Validation(map[string]string{"name": "A storage destination with this name already exists."})
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: orgID, Action: audit.StorageUpdated, ResourceType: "storage", ResourceID: id,
			Metadata: map[string]any{"name": in.Name, "credentials_changed": sealed != nil}})
	})
	if err != nil {
		return Destination{}, err
	}
	return s.Get(ctx, orgID, id)
}

// Delete removes a destination that no schedule or live backup depends on.
// DBVault never silently orphans backups.
func (s *DestinationService) Delete(ctx context.Context, orgID, id string) error {
	d, err := s.Get(ctx, orgID, id)
	if err != nil {
		return err
	}
	if d.ScheduleCount > 0 {
		return apperr.Conflict("This destination is used by schedules. Move or delete those schedules first.")
	}
	if d.BackupCount > 0 {
		return apperr.Conflict("This destination still holds backups. Delete those backups first so nothing is orphaned.")
	}
	return db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE storage_destinations SET deleted_at = now(), is_default = false,
			name = name || ' (deleted ' || to_char(now(), 'YYYY-MM-DD HH24:MI') || ')' WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: orgID, Action: audit.StorageDeleted, ResourceType: "storage", ResourceID: id, Metadata: map[string]any{"name": d.Name}})
	})
}

// TestResult is returned by storage tests.
type TestResult struct {
	OK       bool      `json:"ok"`
	Message  string    `json:"message"`
	Location string    `json:"location"`
	TestedAt time.Time `json:"tested_at"`
}

// TestInput probes an unsaved configuration.
func (s *DestinationService) TestInput(ctx context.Context, in Input) TestResult {
	res := TestResult{TestedAt: time.Now(), Location: describe(in.Type, in.Config)}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	st, err := New(ctx, in.Type, in.Config, in.credentials(), s.Options)
	if err == nil {
		err = Probe(ctx, st, in.Config.Prefix)
	}
	if err != nil {
		res.Message = err.Error()
		return res
	}
	res.OK, res.Message = true, "Write, read and delete succeeded"
	return res
}

// TestSaved probes a stored destination and records the result.
func (s *DestinationService) TestSaved(ctx context.Context, orgID, id string) (TestResult, error) {
	d, err := s.Get(ctx, orgID, id)
	if err != nil {
		return TestResult{}, err
	}
	res := TestResult{TestedAt: time.Now(), Location: d.Location}
	ctx2, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	st, err := s.Open(ctx2, d)
	if err == nil {
		err = Probe(ctx2, st, d.Config.Prefix)
	}
	if err != nil {
		res.Message = err.Error()
	} else {
		res.OK, res.Message = true, "Write, read and delete succeeded"
	}
	var errMsg *string
	if !res.OK {
		errMsg = &res.Message
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE storage_destinations SET last_tested_at = $2, last_test_ok = $3, last_test_error = $4 WHERE id = $1`,
		id, res.TestedAt, res.OK, errMsg)
	return res, nil
}

// Credentials returns an input with the stored credentials of an existing
// destination (used to re-test an edited form without re-entering secrets).
func (s *DestinationService) Credentials(ctx context.Context, orgID, id string) (Credentials, error) {
	d, err := s.Get(ctx, orgID, id)
	if err != nil || d.sealedCreds == nil {
		return Credentials{}, err
	}
	return s.openCreds(d.ID, *d.sealedCreds)
}
