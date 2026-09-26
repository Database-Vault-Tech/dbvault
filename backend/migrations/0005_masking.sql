-- Anonymized restores: restore a backup into a sandbox, mask personal data
-- there with a profile's rules, and hand only the masked copy to the target.

CREATE TABLE masking_profiles (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    database_id      uuid NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
    name             text NOT NULL DEFAULT 'default',
    rules            jsonb NOT NULL,
    version          integer NOT NULL DEFAULT 1,
    updated_by       uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (database_id, name)
);
CREATE INDEX masking_profiles_org_idx ON masking_profiles(organization_id);
CREATE TRIGGER masking_profiles_updated_at BEFORE UPDATE ON masking_profiles
    FOR EACH ROW EXECUTE FUNCTION dbvault_set_updated_at();

-- The schema restored from a backup (tables, columns, keys), recorded during
-- restore tests and masked restores. Powers rule suggestions; never data.
ALTER TABLE backups ADD COLUMN schema_catalog jsonb;

-- Per-organization key that fake values are derived from, sealed with
-- ENCRYPTION_KEY. The same original always gets the same fake.
ALTER TABLE organizations ADD COLUMN masking_key_encrypted text;

ALTER TABLE restore_jobs
    ADD COLUMN masking_profile_id uuid REFERENCES masking_profiles(id) ON DELETE SET NULL,
    ADD COLUMN masking_profile_name text,
    ADD COLUMN masking_profile_version integer,
    ADD COLUMN masking_report jsonb;

ALTER TABLE restore_jobs DROP CONSTRAINT IF EXISTS restore_jobs_status_check;
ALTER TABLE restore_jobs ADD CONSTRAINT restore_jobs_status_check
    CHECK (status IN ('queued', 'running', 'masking', 'verifying', 'completed', 'failed', 'cancelled'));
