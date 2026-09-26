// Package profile stores masking profiles (the rules for one database), the
// per-organization masking key, and serves them to the profile editor.
package profile

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/encryption"
	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/masking"
)

// Audit actions.
const (
	ActionUpdated = "masking_profile.updated"
	ActionDeleted = "masking_profile.deleted"
)

type Service struct {
	Pool    *pgxpool.Pool
	Sealer  *encryption.Sealer
	Drivers *engine.Registry
}

// Profile is a named set of masking rules for one database.
type Profile struct {
	ID             string        `json:"id"`
	DatabaseID     string        `json:"database_id"`
	Name           string        `json:"name"`
	Rules          masking.Rules `json:"rules"`
	Version        int           `json:"version"`
	UpdatedByEmail *string       `json:"updated_by_email"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// ErrNotFound is returned for unknown profiles.
var ErrNotFound = apperr.NotFound("Masking profile")

const selectProfile = `SELECT p.id, p.database_id, p.name, p.rules, p.version, u.email, p.updated_at
	FROM masking_profiles p LEFT JOIN users u ON u.id = p.updated_by`

func scan(row pgx.Row) (Profile, error) {
	var p Profile
	var raw []byte
	if err := row.Scan(&p.ID, &p.DatabaseID, &p.Name, &raw, &p.Version, &p.UpdatedByEmail, &p.UpdatedAt); err != nil {
		return p, err
	}
	return p, json.Unmarshal(raw, &p.Rules)
}

func (s *Service) List(ctx context.Context, orgID, databaseID string) ([]Profile, error) {
	rows, err := s.Pool.Query(ctx, selectProfile+` WHERE p.organization_id = $1 AND p.database_id = $2 ORDER BY p.name`, orgID, databaseID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Profile, error) { return scan(r) })
	if out == nil {
		out = []Profile{}
	}
	return out, err
}

func (s *Service) Get(ctx context.Context, orgID, databaseID, name string) (Profile, error) {
	p, err := scan(s.Pool.QueryRow(ctx, selectProfile+` WHERE p.organization_id = $1 AND p.database_id = $2 AND p.name = $3`, orgID, databaseID, name))
	if db.IsNotFound(err) {
		return p, ErrNotFound
	}
	return p, err
}

// Put creates or replaces a profile, bumping its version.
func (s *Service) Put(ctx context.Context, orgID, userID, databaseID, name string, rules masking.Rules) (Profile, error) {
	if !nameRE.MatchString(name) {
		return Profile{}, apperr.Validation(map[string]string{"name": "Use lowercase letters, numbers, dashes or underscores (max 40)."})
	}
	if problems := rules.Validate(); len(problems) > 0 {
		return Profile{}, apperr.Validation(map[string]string{"rules": problems[0]})
	}
	raw, err := json.Marshal(rules)
	if err != nil {
		return Profile{}, err
	}
	var id string
	var version int
	err = db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO masking_profiles (organization_id, database_id, name, rules, updated_by)
			SELECT $1, $2, $3, $4, $5 WHERE EXISTS (SELECT 1 FROM databases WHERE id = $2 AND organization_id = $1 AND deleted_at IS NULL)
			ON CONFLICT (database_id, name) DO UPDATE SET rules = EXCLUDED.rules, updated_by = EXCLUDED.updated_by,
				version = masking_profiles.version + 1
			RETURNING id, version`, orgID, databaseID, name, raw, userID).Scan(&id, &version)
		if db.IsNotFound(err) {
			return apperr.NotFound("Database")
		}
		if err != nil {
			return err
		}
		tables := sortedTables(rules)
		return audit.Record(ctx, tx, audit.Entry{OrgID: orgID, Action: ActionUpdated, ResourceType: "masking_profile", ResourceID: id,
			Metadata: map[string]any{"database_id": databaseID, "name": name, "version": version, "tables": tables}})
	})
	if err != nil {
		return Profile{}, err
	}
	return s.Get(ctx, orgID, databaseID, name)
}

func (s *Service) Delete(ctx context.Context, orgID, databaseID, name string) error {
	return db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `DELETE FROM masking_profiles WHERE organization_id = $1 AND database_id = $2 AND name = $3 RETURNING id`,
			orgID, databaseID, name).Scan(&id)
		if db.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: orgID, Action: ActionDeleted, ResourceType: "masking_profile", ResourceID: id,
			Metadata: map[string]any{"database_id": databaseID, "name": name}})
	})
}

func sortedTables(r masking.Rules) []string {
	out := make([]string, 0, len(r.Tables))
	for t := range r.Tables {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func keyAAD(orgID string) string { return "organization:" + orgID + ":masking-key" }

// Key returns the organization's masking key, creating it on first use.
func (s *Service) Key(ctx context.Context, orgID string) (string, error) {
	var sealed *string
	if err := s.Pool.QueryRow(ctx, `SELECT masking_key_encrypted FROM organizations WHERE id = $1`, orgID).Scan(&sealed); err != nil {
		return "", err
	}
	if sealed == nil {
		key, err := masking.NewKey()
		if err != nil {
			return "", err
		}
		v, err := s.Sealer.SealString(key, keyAAD(orgID))
		if err != nil {
			return "", err
		}
		// Two jobs may race here; whichever stored a key first wins.
		if _, err := s.Pool.Exec(ctx, `UPDATE organizations SET masking_key_encrypted = $2 WHERE id = $1 AND masking_key_encrypted IS NULL`, orgID, v); err != nil {
			return "", err
		}
		if err := s.Pool.QueryRow(ctx, `SELECT masking_key_encrypted FROM organizations WHERE id = $1`, orgID).Scan(&sealed); err != nil {
			return "", err
		}
	}
	return s.Sealer.OpenString(*sealed, keyAAD(orgID))
}

// SchemaSource is where a database's catalog came from.
type SchemaSource struct {
	BackupID   string    `json:"backup_id"`
	BackupAt   time.Time `json:"backup_created_at"`
	RecordedAt time.Time `json:"recorded_at"`
}

// LatestCatalog returns the schema recorded from the newest backup that has
// one, or nil when none has been restored yet.
func (s *Service) LatestCatalog(ctx context.Context, orgID, databaseID string) (*engine.Catalog, *SchemaSource, error) {
	var raw []byte
	var src SchemaSource
	err := s.Pool.QueryRow(ctx, `SELECT id, created_at, COALESCE(verified_at, completed_at, created_at), schema_catalog FROM backups
		WHERE organization_id = $1 AND database_id = $2 AND schema_catalog IS NOT NULL ORDER BY created_at DESC LIMIT 1`,
		orgID, databaseID).Scan(&src.BackupID, &src.BackupAt, &src.RecordedAt, &raw)
	if db.IsNotFound(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var cat engine.Catalog
	if err := json.Unmarshal(raw, &cat); err != nil {
		return nil, nil, err
	}
	return &cat, &src, nil
}

// RecordCatalog stores the schema restored from a backup.
func RecordCatalog(ctx context.Context, pool *pgxpool.Pool, backupID string, cat engine.Catalog) error {
	raw, err := json.Marshal(cat)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `UPDATE backups SET schema_catalog = $2 WHERE id = $1`, backupID, raw)
	return err
}

// Supported reports whether masked restores work for an engine, and why not.
func (s *Service) Supported(engineName string) (bool, string) {
	drv, err := s.Drivers.Get(engineName)
	if err != nil {
		return false, "Unknown database type."
	}
	if _, ok := drv.(masking.Masker); !ok {
		return false, "Masked restores aren't available for " + drv.Label() + " yet. PostgreSQL and SQLite are supported; MySQL and MariaDB are next."
	}
	return true, ""
}

// Problems resolves rules against a catalog and returns why a masked restore
// would stop (empty when it would run).
func Problems(rules masking.Rules, cat *engine.Catalog) []string {
	if cat == nil {
		return rules.Validate()
	}
	_, err := masking.Build(rules, *cat, "preview")
	var p masking.Problems
	if errors.As(err, &p) {
		return p
	}
	return []string{}
}
