# Design: anonymized restores

Status: **phase 1 built** (PostgreSQL and SQLite). The user guide is
[Data masking](../masking.md). Refinements made while building are noted inline.

## The problem

Teams want realistic data in staging and on laptops, and they must not copy real customer
data there. Today the choices are a hand-written seed script that drifts from production, or
a production dump that leaks emails, names and payment details into places with weaker
security.

DBVault already has a nightly backup of production and can restore it into a disposable
sandbox. Anonymized restores add one step in between: **restore → mask inside the sandbox →
hand over only the masked copy.**

```text
dbvault restore <backup> --new-database staging --mask default
```

## Goals

1. Real values never leave the sandbox. Staging only ever receives masked data.
2. Masking is consistent: the same real email becomes the same fake email in every table and
   on every run, so joins, lookups and "log in as this test user" keep working.
3. Setup takes minutes: DBVault suggests the rules by recognizing personal columns.
4. It fails closed. A new column that looks personal but has no rule blocks the run instead
   of leaking silently.
5. Every run says what it did: tables and rows touched, rules applied, checks passed.

## Not in the first version

- **Subsetting** ("keep 10% of rows") with foreign-key integrity. It needs a dependency-graph
  walk and is a feature of its own; it comes in phase 3.
- **Masking key columns** (primary and foreign keys). Rules on them are rejected at first.
- **Custom SQL expressions** as rules. The built-in rule types cover the common cases.
- **MySQL and MariaDB** (phase 2, see [Engines](#engines)).

## How a masked restore runs

A masked restore is a normal restore job with extra steps. The new steps are 3–6:

| # | Step | Where | Notes |
|---|---|---|---|
| 1 | Download the backup and check its SHA-256 | worker | Existing code |
| 2 | Restore into a sandbox database | sandbox | The same sandboxes restore testing uses |
| 3 | Check the rules against the restored schema | sandbox | Fails on missing tables and columns, and on unruled personal columns (drift) |
| 4 | Apply the rules | sandbox | Triggers off, one transaction per table |
| 5 | Verify the masking | sandbox | E.g. no value in a column masked as `email` still looks like the original |
| 6 | Dump the sandbox | sandbox → worker | The engine's normal dump tool; only live rows are dumped |
| 7 | Restore the masked dump into the target | target | Existing restore code: a new database, or an existing one |
| 8 | Destroy the sandbox | sandbox | Always, even on failure or cancellation |

The masked dump in step 6 streams straight into step 7 and is never written to backup
storage. Real data exists only inside the sandbox and in the encrypted backup it came from.

**Why not mask the dump as it streams?** Rewriting `pg_dump`'s custom format (or SQL text)
on the fly means parsing every engine's dump format and data encoding. Masking inside a real
database uses the database's own types, functions and constraints, and the result is checked
by the database before anything leaves.

**Why not restore to the target first and mask there?** Then real data sits in staging while
masking runs, and a failure leaves it there.

**Cost:** a masked restore takes about twice as long as a plain one: two restores plus the
UPDATEs. Truncating large log and event tables (a rule type) usually recovers most of that.

**When no sandbox is available** (`VERIFY_MODE=disabled`), masked restores are unavailable
and the UI says why. We never fall back to masking in the target. SQLite doesn't need the
setting: its sandbox is always a temporary file on the worker.

## Rules

A **masking profile** belongs to one database and lists what happens to each table and
column. Anything not listed is copied unchanged, except columns that look personal (see
[Fail closed](#fail-closed-on-schema-drift)).

```yaml
tables:
  users:
    email: email
    full_name: name
    phone: phone
    date_of_birth: date_shift
    password_hash: { redact: "$argon2id$masked" }
  payments: truncate
  audit_events: truncate
  sessions: truncate
```

| Rule | Result | Deterministic |
|---|---|---|
| `email` | `user_3f9a2c71@example.com` | yes |
| `name` | A realistic name from a built-in list, e.g. `Maria Okafor` | yes |
| `first_name`, `last_name` | One part of a name | yes |
| `phone` | `+1 555 0134 912`: same length, reserved `555` range | yes |
| `hash` | Hex of the keyed hash, cut to the column's length | yes |
| `date_shift` | The date moved by up to ±180 days (the same shift per value) | yes |
| `redact: <value>` | A constant, e.g. `REDACTED` | — |
| `null` | `NULL` (only on nullable columns) | — |
| `truncate` (table) | The table is emptied; the schema stays | — |

Values in a column that are already `NULL` stay `NULL`.

### Deterministic fakes

Every fake is derived from a keyed hash of the original:

```text
h = SHA-256(masking_key || original)
```

- The **masking key** is 32 random bytes per organization, generated on first use and sealed
  with `ENCRYPTION_KEY` like every other secret. Without it, nobody can confirm a guess by
  hashing a candidate email, which a plain hash would allow.
- The same original always gives the same fake: across tables, across runs and across
  engines. Staging data stays stable from night to night.
- Collisions are negligible for `email` and `hash`, so unique constraints keep holding.
  `name` and `phone` can collide by design; a rule on a column with a unique constraint is
  rejected unless the rule keeps values unique (`email`, `hash`).
- Hash functions per engine: PostgreSQL 11+ `sha256()`, MySQL `SHA2(…, 256)`, SQLite a Go
  function registered on the connection (the driver runs in-process). PostgreSQL 9.2–10
  sandboxes use `md5()`. That's weaker, but the value is keyed and only used to pick a fake,
  never for integrity.

### Suggested rules

The profile editor shows every table and column of the database, taken from the schema of
its latest backup. That schema is recorded during restore tests, so no extra restore is
needed. Columns whose names or types look personal are flagged and get a suggested rule:

| Pattern (column name) | Suggested rule |
|---|---|
| `email`, `*_email` | `email` |
| `name`, `full_name`, `first_name`, `last_name`, `display_name` | `name` / `first_name` / `last_name` |
| `phone`, `mobile`, `*_phone` | `phone` |
| `address*`, `street`, `postcode`, `zip` | `redact` |
| `ssn`, `national_id`, `passport*`, `tax_id` | `hash` |
| `card*`, `iban`, `account_number` | `redact` |
| `password*`, `*_token`, `*_secret`, `api_key` | `redact` |
| `ip`, `ip_address`, `last_ip` | `hash` |
| `dob`, `birth*`, `date_of_birth` | `date_shift` |
| tables `sessions`, `*_logs`, `audit*`, `events` | `truncate` |

Suggestions are pre-filled and must be saved by a person; nothing is masked without a saved
profile.

### Fail closed on schema drift

A migration adds `users.recovery_email`, and nobody updates the profile. Before masking,
step 3 compares the restored schema with the profile:

- A table or column named in a rule no longer exists → **fail**, naming it.
- A column that matches a personal pattern has no rule → **fail**, naming it, unless the
  profile marks it `keep` (an explicit "this is fine").
- Other columns without a rule are copied unchanged. *(Built: no separate warning, since
  DBVault doesn't compare against the previous schema; the fail-closed check above covers
  the risky case.)*

That makes a scheduled "refresh staging nightly" job stop instead of leaking.

## Engines

| | PostgreSQL | SQLite | MySQL / MariaDB |
|---|---|---|---|
| Phase | 1 | 1 | 2 |
| Sandbox | verify server or Docker container (superuser) | temporary file on the worker | verify server or Docker container |
| Triggers during masking | off: `SET session_replication_role = replica` | dropped and recreated from their stored SQL (`sqlite_schema`) in the same transaction as the masking | must be saved, dropped and recreated around masking (no session switch) |
| Leftover original values | none: `pg_dump` exports live rows only | removed: the file is rebuilt with `VACUUM INTO` | none: `mariadb-dump` exports live rows only |

MySQL waits for phase 2 because it has no way to switch triggers off for one session, and an
audit trigger that copies the old row elsewhere would write the real value into another
table. Saving, dropping and recreating triggers works, but deserves its own tests.

## Safety rules

- **Never into the source.** A masked restore can't target the database the backup was taken
  from, so production can't be overwritten with fake data.
- **Admins only**, like restores. Editing a profile is audited, with a diff of the rules.
- **No real values in the UI or the API.** The editor shows rule examples on made-up input
  (`ada@example.com → user_3f9a2c71@example.com`), never rows from the backup. Run reports
  contain counts and column names only.
- **Checked output.** Step 5 queries the sandbox after masking: for example, no row in a
  column masked as `email` may be outside `@example.com`, and truncated tables must be
  empty. A failed check fails the run, and nothing reaches the target.
- **Profiles are versioned.** Each run records the profile version it used.

## Data model

```sql
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

-- Schema of a backup, recorded during restore tests: powers suggestions and drift checks.
ALTER TABLE backups ADD COLUMN schema_catalog jsonb;

-- Per-organization key for deterministic fakes, sealed with ENCRYPTION_KEY.
ALTER TABLE organizations ADD COLUMN masking_key_encrypted text;

ALTER TABLE restore_jobs
    ADD COLUMN masking_profile_id uuid REFERENCES masking_profiles(id) ON DELETE SET NULL,
    ADD COLUMN masking_profile_version integer,
    ADD COLUMN masking_report jsonb;
-- New status between running and verifying:
--   queued → running → masking → verifying → completed
```

## API, UI and CLI

**API**

- `GET /databases/{id}/masking-profiles`, `PUT /databases/{id}/masking-profiles/{name}`
- `POST /databases/{id}/masking-profiles/suggest`: suggested rules from the latest schema
  catalog
- `POST /restores` gains `masking_profile`, e.g. `{"mode": "new", "new_database_name":
  "staging", "masking_profile": "default"}`

**UI**

- A **Masking** tab on each database: tables and columns, personal columns flagged, a rule
  picker per column, and example output per rule. "Apply suggestions" fills the gaps.
- The restore wizard gets a "Mask personal data" switch, which is on by default when a
  profile exists and the target is a different database from the backup's. *(Refined: a
  restore into a new database on the source's own server, typically disaster recovery,
  stays unmasked unless switched on.)*
- Restore details show the masking report: tables and rows changed, rules applied, checks
  passed, and drift warnings.

**CLI**

```text
dbvault restore <backup-id> --new-database staging --mask default
dbvault masking suggest <database>     # print suggested rules as YAML
dbvault masking apply <database> -f masking.yaml
```

Profiles as YAML files let teams keep them in git next to their migrations.

## Phases

1. **PostgreSQL and SQLite.** Profiles, suggestions, fail-closed drift checks, masked
   restores into a new or existing (non-source) database, run reports, and the UI, CLI and
   API above.
2. **MySQL and MariaDB** (trigger handling). **Scheduled refreshes**: "restore last night's
   backup, masked, into staging every morning". **Masked downloads**: a masked dump
   developers can load on a laptop.
3. **Subsetting** with foreign-key integrity, rules on key columns, and custom expressions.

## Decisions

1. **Phase 1 engines:** PostgreSQL and SQLite.
2. **Masked downloads:** phase 2, with scheduled refreshes.
3. **Strictness:** fail closed, always (no warn-only switch yet).
4. **Where profiles live:** in DBVault, edited in the UI, with YAML export and import through
   the CLI (`dbvault masking suggest | show | apply`).
