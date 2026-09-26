package restore

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dbvault/dbvault/backend/internal/backups"
	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/jobs"
	"github.com/dbvault/dbvault/backend/internal/masking"
	"github.com/dbvault/dbvault/backend/internal/masking/profile"
)

// maskedCopy is a sandbox holding a masked copy of a backup, ready to be
// dumped into the restore target.
type maskedCopy struct {
	inst   *Instance
	source engine.Target // the sandbox database
	info   engine.ServerInfo
}

func (m *maskedCopy) destroy(log *jobs.Logger) {
	if err := m.inst.Destroy(); err != nil {
		log.Warnf("Could not remove the masking sandbox: %v", err)
		return
	}
	log.Infof("Masking sandbox destroyed; no unmasked data remains outside the encrypted backup")
}

// maskInSandbox restores the backup into a disposable sandbox, checks the
// profile against the schema it finds there, masks, and checks the result.
// Real values never leave the sandbox.
func (s *Service) maskInSandbox(ctx context.Context, r Job, b backups.Backup, drv engine.Driver, src archiveSource, log *jobs.Logger) (*maskedCopy, error) {
	masker, ok := drv.(masking.Masker)
	if !ok {
		return nil, fmt.Errorf("masked restores aren't available for %s yet", drv.Label())
	}
	sandbox := s.Sandbox
	if drv.Capabilities().FileBased {
		sandbox = FileSandbox{WorkDir: s.WorkDir}
	}
	if sandbox == nil {
		return nil, fmt.Errorf("masked restores need a restore sandbox, and restore testing is disabled on this server")
	}
	if err := sandbox.Check(ctx, drv); err != nil {
		return nil, fmt.Errorf("the restore sandbox isn't available: %w", err)
	}
	prof, err := s.Profiles.Get(ctx, r.organizationID, r.sourceDatabaseID, *r.MaskingProfile)
	if err != nil {
		return nil, fmt.Errorf("masking profile %q: %w", *r.MaskingProfile, err)
	}
	key, err := s.Profiles.Key(ctx, r.organizationID)
	if err != nil {
		return nil, err
	}

	major := 0
	if b.PGVersion != nil {
		major, _ = strconv.Atoi(strings.SplitN(*b.PGVersion, ".", 2)[0])
	}
	log.Infof("Starting masking sandbox (%s, %s %d)", sandbox.Name(), drv.Label(), major)
	inst, err := sandbox.Provision(ctx, drv, major)
	if err != nil {
		return nil, fmt.Errorf("could not start the masking sandbox: %w", err)
	}
	m := &maskedCopy{inst: inst}
	log.Infof("Restoring the backup into the sandbox (%s)", inst.Description)
	if err := runRestore(ctx, drv, src, inst.Target, inst.DBName, engine.RestoreOptions{Atomic: true}, log); err != nil {
		return m, fmt.Errorf("restoring into the masking sandbox: %w", err)
	}

	cat, err := masker.Catalog(ctx, inst.Target, inst.DBName)
	if err != nil {
		return m, fmt.Errorf("reading the backup's schema: %w", err)
	}
	if err := profile.RecordCatalog(ctx, s.Pool, b.ID, cat); err != nil {
		log.Warnf("Could not record the backup's schema: %v", err)
	}
	plan, err := masking.Build(prof.Rules, cat, key)
	if err != nil {
		// Fail closed: e.g. a new personal-looking column without a rule.
		return m, err
	}

	_, _ = s.Pool.Exec(ctx, `UPDATE restore_jobs SET status = 'masking', masking_profile_version = $2 WHERE id = $1`, r.ID, prof.Version)
	log.Infof("Masking with profile %q (version %d): %d tables", prof.Name, prof.Version, len(plan.Tables))
	rep, err := masker.Mask(ctx, inst.Target, inst.DBName, plan, log)
	if err != nil {
		return m, err
	}
	rep.ProfileVersion = prof.Version
	raw, _ := json.Marshal(rep)
	_, _ = s.Pool.Exec(ctx, `UPDATE restore_jobs SET masking_report = $2 WHERE id = $1`, r.ID, raw)
	log.Infof("Masking done: %d rows changed, %d checks passed", rep.RowsChanged, rep.Checks)

	m.source = inst.Target
	m.source.Database = inst.DBName
	if m.info, err = drv.Inspect(ctx, m.source); err != nil {
		return m, err
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE restore_jobs SET status = 'running' WHERE id = $1`, r.ID)
	return m, nil
}

// restoreInto streams a dump of the masked sandbox straight into the target;
// the masked copy is never written to storage.
func (m *maskedCopy) restoreInto(ctx context.Context, drv engine.Driver, target engine.Target, dbName string, opts engine.RestoreOptions, log *jobs.Logger) error {
	dctx, cancel := context.WithCancel(ctx)
	defer cancel()
	dump, err := drv.Dump(dctx, m.source, m.info)
	if err != nil {
		return fmt.Errorf("dumping the masked copy: %w", err)
	}
	log.Infof("Restoring the masked copy into %q", dbName)
	restoreErr := drv.Restore(ctx, target, dbName, dump.Stream, opts, log)
	if restoreErr != nil {
		cancel() // stop the dump if the restore gave up early
	}
	waitErr := dump.Wait()
	if restoreErr != nil {
		return restoreErr
	}
	if waitErr != nil {
		return fmt.Errorf("dumping the masked copy: %w", waitErr)
	}
	return nil
}
