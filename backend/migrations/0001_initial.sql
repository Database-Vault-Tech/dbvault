-- DBVault initial schema.
-- All primary keys are UUIDs; all mutable tables carry created_at/updated_at.

CREATE EXTENSION IF NOT EXISTS citext;

CREATE OR REPLACE FUNCTION dbvault_set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- Identity
-- ---------------------------------------------------------------------------

CREATE TABLE users (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email           citext NOT NULL UNIQUE,
    name            text NOT NULL,
    password_hash   text NOT NULL,
    last_login_at   timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      bytea NOT NULL UNIQUE,
    ip_address      text,
    user_agent      text,
    expires_at      timestamptz NOT NULL,
    last_seen_at    timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);

CREATE TABLE password_reset_tokens (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      bytea NOT NULL UNIQUE,
    expires_at      timestamptz NOT NULL,
    used_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX password_reset_tokens_user_id_idx ON password_reset_tokens(user_id);

-- ---------------------------------------------------------------------------
-- Organizations
-- ---------------------------------------------------------------------------

CREATE TABLE organizations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name            text NOT NULL,
    slug            text NOT NULL UNIQUE,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE organization_members (
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role            text NOT NULL CHECK (role IN ('owner', 'admin', 'member', 'viewer')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, user_id)
);
CREATE INDEX organization_members_user_id_idx ON organization_members(user_id);

CREATE TABLE organization_invitations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email           citext NOT NULL,
    role            text NOT NULL CHECK (role IN ('admin', 'member', 'viewer')),
    token_hash      bytea NOT NULL UNIQUE,
    invited_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    expires_at      timestamptz NOT NULL,
    accepted_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX organization_invitations_org_idx ON organization_invitations(organization_id);

CREATE TABLE api_tokens (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            text NOT NULL,
    token_prefix    text NOT NULL,
    token_hash      bytea NOT NULL UNIQUE,
    last_used_at    timestamptz,
    expires_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX api_tokens_user_id_idx ON api_tokens(user_id);

-- Per-organization backup encryption keys (age X25519). The private key is
-- sealed with the master ENCRYPTION_KEY and never stored in plaintext.
CREATE TABLE encryption_keys (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    algorithm             text NOT NULL DEFAULT 'age-x25519',
    public_key            text NOT NULL,
    private_key_encrypted text NOT NULL,
    active                boolean NOT NULL DEFAULT true,
    created_at            timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX encryption_keys_one_active_idx ON encryption_keys(organization_id) WHERE active;

-- ---------------------------------------------------------------------------
-- Protected databases and storage
-- ---------------------------------------------------------------------------

CREATE TABLE databases (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name                text NOT NULL,
    host                text NOT NULL,
    port                integer NOT NULL CHECK (port BETWEEN 1 AND 65535),
    database_name       text NOT NULL,
    username            text NOT NULL,
    password_encrypted  text NOT NULL,
    ssl_mode            text NOT NULL DEFAULT 'prefer'
                        CHECK (ssl_mode IN ('disable', 'allow', 'prefer', 'require', 'verify-ca', 'verify-full')),
    ssl_root_cert       text,
    pg_version          text,
    size_bytes          bigint,
    last_tested_at      timestamptz,
    last_test_ok        boolean,
    last_test_error     text,
    created_by          uuid REFERENCES users(id) ON DELETE SET NULL,
    deleted_at          timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX databases_org_name_idx ON databases(organization_id, lower(name)) WHERE deleted_at IS NULL;
CREATE INDEX databases_org_idx ON databases(organization_id) WHERE deleted_at IS NULL;

CREATE TABLE storage_destinations (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name                    text NOT NULL,
    type                    text NOT NULL CHECK (type IN ('local', 's3', 'r2', 'minio')),
    config                  jsonb NOT NULL DEFAULT '{}',
    credentials_encrypted   text,
    is_default              boolean NOT NULL DEFAULT false,
    last_tested_at          timestamptz,
    last_test_ok            boolean,
    last_test_error         text,
    created_by              uuid REFERENCES users(id) ON DELETE SET NULL,
    deleted_at              timestamptz,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX storage_destinations_org_name_idx ON storage_destinations(organization_id, lower(name)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX storage_destinations_one_default_idx ON storage_destinations(organization_id) WHERE is_default AND deleted_at IS NULL;

CREATE TABLE backup_schedules (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    database_id             uuid NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
    storage_destination_id  uuid NOT NULL REFERENCES storage_destinations(id) ON DELETE RESTRICT,
    name                    text NOT NULL,
    preset                  text NOT NULL CHECK (preset IN ('hourly', 'every_6_hours', 'daily', 'weekly', 'custom')),
    cron_expression         text NOT NULL,
    timezone                text NOT NULL DEFAULT 'UTC',
    enabled                 boolean NOT NULL DEFAULT true,
    compression             text NOT NULL DEFAULT 'zstd' CHECK (compression IN ('zstd', 'gzip', 'none')),
    encryption              boolean NOT NULL DEFAULT true,
    retention_daily         integer NOT NULL DEFAULT 7 CHECK (retention_daily BETWEEN 0 AND 3650),
    retention_weekly        integer NOT NULL DEFAULT 4 CHECK (retention_weekly BETWEEN 0 AND 520),
    retention_monthly       integer NOT NULL DEFAULT 6 CHECK (retention_monthly BETWEEN 0 AND 240),
    verify_after_backup     boolean NOT NULL DEFAULT false,
    next_run_at             timestamptz,
    last_run_at             timestamptz,
    created_by              uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX backup_schedules_due_idx ON backup_schedules(next_run_at) WHERE enabled;
CREATE INDEX backup_schedules_database_idx ON backup_schedules(database_id);

-- ---------------------------------------------------------------------------
-- Jobs (backup_jobs in the product spec: one table for every job type)
-- ---------------------------------------------------------------------------

CREATE TABLE jobs (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid REFERENCES organizations(id) ON DELETE CASCADE,
    type                text NOT NULL CHECK (type IN ('backup', 'restore', 'verification', 'cleanup', 'notification')),
    status              text NOT NULL DEFAULT 'queued'
                        CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
    payload             jsonb NOT NULL DEFAULT '{}',
    result              jsonb,
    progress            jsonb,
    error               text,
    attempts            integer NOT NULL DEFAULT 0,
    max_attempts        integer NOT NULL DEFAULT 1,
    cancel_requested    boolean NOT NULL DEFAULT false,
    worker_id           text,
    heartbeat_at        timestamptz,
    run_after           timestamptz NOT NULL DEFAULT now(),
    -- Last time the job id was pushed to the Redis queue. NULL means it still
    -- needs dispatching; the scheduler's sweeper re-pushes stale queued jobs,
    -- so a lost Redis message can never strand a job.
    pushed_at           timestamptz,
    started_at          timestamptz,
    completed_at        timestamptz,
    created_by          uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX jobs_org_created_idx ON jobs(organization_id, created_at DESC);
CREATE INDEX jobs_status_idx ON jobs(status, run_after) WHERE status IN ('queued', 'running');

CREATE TABLE job_logs (
    id          bigserial PRIMARY KEY,
    job_id      uuid NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    level       text NOT NULL DEFAULT 'info' CHECK (level IN ('debug', 'info', 'warn', 'error')),
    message     text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX job_logs_job_idx ON job_logs(job_id, id);

-- ---------------------------------------------------------------------------
-- Backups
-- ---------------------------------------------------------------------------

CREATE TABLE backups (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    database_id             uuid NOT NULL REFERENCES databases(id) ON DELETE RESTRICT,
    schedule_id             uuid REFERENCES backup_schedules(id) ON DELETE SET NULL,
    storage_destination_id  uuid NOT NULL REFERENCES storage_destinations(id) ON DELETE RESTRICT,
    job_id                  uuid REFERENCES jobs(id) ON DELETE SET NULL,
    trigger                 text NOT NULL CHECK (trigger IN ('manual', 'scheduled')),
    status                  text NOT NULL DEFAULT 'queued'
                            CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled', 'deleted')),
    storage_key             text,
    format                  text NOT NULL DEFAULT 'pg_dump_custom',
    compression             text NOT NULL CHECK (compression IN ('zstd', 'gzip', 'none')),
    encrypted               boolean NOT NULL,
    encryption_key_id       uuid REFERENCES encryption_keys(id) ON DELETE RESTRICT,
    size_bytes              bigint,
    raw_size_bytes          bigint,
    checksum_sha256         text,
    pg_version              text,
    pg_dump_version         text,
    table_count             integer,
    error                   text,
    started_at              timestamptz,
    completed_at            timestamptz,
    duration_ms             bigint,
    verification_status     text NOT NULL DEFAULT 'none'
                            CHECK (verification_status IN ('none', 'running', 'passed', 'failed', 'unavailable')),
    verification            jsonb,
    verified_at             timestamptz,
    deleted_at              timestamptz,
    deleted_reason          text,
    created_by              uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX backups_org_created_idx ON backups(organization_id, created_at DESC);
CREATE INDEX backups_database_created_idx ON backups(database_id, created_at DESC);
CREATE INDEX backups_schedule_idx ON backups(schedule_id, status);
CREATE INDEX backups_active_idx ON backups(status) WHERE status IN ('queued', 'running');

-- ---------------------------------------------------------------------------
-- Restores
-- ---------------------------------------------------------------------------

CREATE TABLE restore_jobs (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    job_id              uuid REFERENCES jobs(id) ON DELETE SET NULL,
    backup_id           uuid NOT NULL REFERENCES backups(id) ON DELETE RESTRICT,
    target_database_id  uuid NOT NULL REFERENCES databases(id) ON DELETE RESTRICT,
    mode                text NOT NULL CHECK (mode IN ('existing', 'new')),
    new_database_name   text,
    status              text NOT NULL DEFAULT 'queued'
                        CHECK (status IN ('queued', 'running', 'verifying', 'completed', 'failed', 'cancelled')),
    error               text,
    verification        jsonb,
    started_at          timestamptz,
    completed_at        timestamptz,
    duration_ms         bigint,
    requested_by        uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK (mode = 'existing' OR new_database_name IS NOT NULL)
);
CREATE INDEX restore_jobs_org_created_idx ON restore_jobs(organization_id, created_at DESC);
CREATE INDEX restore_jobs_backup_idx ON restore_jobs(backup_id);

-- ---------------------------------------------------------------------------
-- Notifications
-- ---------------------------------------------------------------------------

CREATE TABLE notifications (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name                text NOT NULL,
    type                text NOT NULL CHECK (type IN ('email', 'webhook')),
    config              jsonb NOT NULL DEFAULT '{}',
    secret_encrypted    text,
    events              text[] NOT NULL DEFAULT '{}',
    enabled             boolean NOT NULL DEFAULT true,
    created_by          uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_org_idx ON notifications(organization_id);

CREATE TABLE notification_deliveries (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    notification_id     uuid NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    job_id              uuid REFERENCES jobs(id) ON DELETE SET NULL,
    event               text NOT NULL,
    status              text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts            integer NOT NULL DEFAULT 0,
    response_status     integer,
    error               text,
    payload             jsonb NOT NULL,
    delivered_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notification_deliveries_org_created_idx ON notification_deliveries(organization_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- Audit log (append-only)
-- ---------------------------------------------------------------------------

CREATE TABLE audit_logs (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid REFERENCES organizations(id) ON DELETE CASCADE,
    actor_type          text NOT NULL CHECK (actor_type IN ('user', 'api_token', 'system')),
    actor_id            uuid,
    actor_email         text,
    action              text NOT NULL,
    resource_type       text,
    resource_id         uuid,
    metadata            jsonb NOT NULL DEFAULT '{}',
    ip_address          text,
    user_agent          text,
    created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_org_created_idx ON audit_logs(organization_id, created_at DESC);
CREATE INDEX audit_logs_org_action_idx ON audit_logs(organization_id, action);

-- ---------------------------------------------------------------------------
-- updated_at triggers
-- ---------------------------------------------------------------------------

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['users', 'organizations', 'organization_members', 'databases',
                             'storage_destinations', 'backup_schedules', 'jobs', 'backups',
                             'restore_jobs', 'notifications', 'notification_deliveries']
    LOOP
        EXECUTE format('CREATE TRIGGER %I_set_updated_at BEFORE UPDATE ON %I
                        FOR EACH ROW EXECUTE FUNCTION dbvault_set_updated_at()', t, t);
    END LOOP;
END $$;
