-- Two-factor sign-in: TOTP (RFC 6238) authenticator codes plus single-use
-- recovery codes.

-- The TOTP secret is sealed with the master ENCRYPTION_KEY. It is written at
-- setup and only takes effect once totp_enabled_at is set (after the user
-- proves their authenticator works). totp_last_step is the last accepted
-- 30-second time step, so a code can never be used twice.
ALTER TABLE users
    ADD COLUMN totp_secret_encrypted text,
    ADD COLUMN totp_enabled_at       timestamptz,
    ADD COLUMN totp_last_step        bigint;

-- Only HMAC digests (keyed with AUTH_SECRET) of recovery codes are stored.
CREATE TABLE user_recovery_codes (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash       bytea NOT NULL,
    used_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, code_hash)
);

-- A password check that still needs a second factor. The browser holds the
-- plaintext token for a few minutes; no session exists until a valid code
-- is presented.
CREATE TABLE mfa_challenges (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      bytea NOT NULL UNIQUE,
    attempts        integer NOT NULL DEFAULT 0,
    expires_at      timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mfa_challenges_user_id_idx ON mfa_challenges(user_id);
CREATE INDEX mfa_challenges_expires_at_idx ON mfa_challenges(expires_at);
