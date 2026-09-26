# Data masking

Anonymized restores copy production into staging or development with personal data
replaced by realistic fakes. DBVault restores the backup into a disposable sandbox, masks
it there, checks the result, and only then copies the masked database to the target. Real
values never reach the target.

Available for **PostgreSQL** and **SQLite**. MySQL and MariaDB are next.

## Set it up

1. **Verify a backup** of the database once (*Verify backup* on any backup). DBVault
   records the tables and columns it restored (never any data). That schema powers the
   suggestions below.
2. Open the database and click **Set up masking**. DBVault flags the columns that look
   personal and suggests a rule for each: emails, names, phone numbers, addresses, card
   numbers, passwords and tokens, IP addresses, dates of birth, and tables such as sessions
   and logs.
3. Review the rules, change what you need, and **Save**.

Then restore as usual. When the target is a different database from the backup's, the
restore wizard switches on **Mask personal data** automatically:

```bash
dbvault restore <backup-id> --target staging --existing --mask default
```

The restore's details show what masking did: rows changed per table, the rules applied,
and the checks that passed.

## Rules

| Rule | Result |
|---|---|
| Fake email (`email`) | `user_3f9a2c71@example.com` |
| Fake full name (`name`), first name, last name | A realistic name from a built-in list |
| Fake phone (`phone`) | `+1 555 013 4912`, in the reserved 555 range |
| Hash (`hash`) | A keyed hash, shortened to fit the column. Unique values stay unique. |
| Shift date (`date_shift`) | The date moved by up to ±180 days |
| Replace with (`redact`) | A fixed value you choose, such as `REDACTED` |
| Empty (`null`) | `NULL`, for nullable columns |
| Keep (`keep`) | Copied unchanged: you checked the column isn't personal |
| Empty the table (`truncate`) | Removes every row; the table stays |

`NULL` values stay `NULL`. Columns without a rule are copied unchanged.

### The same person gets the same fake

Fakes are derived from the original value and a secret key that belongs to your
organization. So the same email becomes the same fake email in every table and on every
restore. Joins still work, and a test user keeps their fake identity from one night's
refresh to the next. Without the key, a fake can't be traced back to the original by
guessing.

### What DBVault refuses

A masked restore **stops before touching the target** when:

- **A personal-looking column has no rule.** This is usually a column added by a migration
  since you set up masking. Give it a rule, or choose *Keep (reviewed)* if it's fine.
- **A rule names a table or column that no longer exists.**
- **A rule can't apply to its column.** Key columns are always copied so relationships keep
  working. Unique columns need a rule that keeps values unique (`email`, `hash` or `null`).
  Text rules need text columns, and `date_shift` needs a date or timestamp column.
- **You empty a table that other tables still point at.** Empty those tables too, or mask
  the columns instead.
- **Overwriting the backup's own database.** A masked restore can never replace production
  with fake data.

The masking editor shows these problems before you run anything.

## Keep rules in git

Profiles are plain YAML, so they can live next to your migrations:

```yaml
tables:
  users:
    email: email
    full_name: name
    phone: phone
    date_of_birth: date_shift
    password_hash: { redact: masked }
  sessions: truncate
```

```bash
dbvault masking suggest production > masking.yaml   # start from the suggestions
dbvault masking apply production -f masking.yaml    # save (prints any problems)
dbvault masking show production
```

*Export YAML* in the editor downloads the same file.

## How it works

1. The backup is downloaded and its checksum verified.
2. It's restored into a sandbox: the same verification server or container that restore
   tests use, or a temporary file for SQLite.
3. The rules are checked against the schema actually restored.
4. Masking runs in one transaction, with triggers switched off so audit triggers can't copy
   real values elsewhere.
5. Checks confirm the result: for example, every masked email is a generated one, and
   emptied tables are empty. If anything fails, the whole run is rolled back.
6. The masked sandbox is dumped and streamed straight into the target. It's never written to
   backup storage.
7. The sandbox is destroyed.

Masked restores take about twice as long as normal ones: they restore twice. Emptying large
log and event tables usually wins most of that back.

**Requirements.** PostgreSQL masking needs restore testing (`VERIFY_MODE=server` or
`docker`), because it runs in the verification sandbox as a superuser. SQLite masking always
works. PostgreSQL 9.2–10 sandboxes use a keyed MD5 instead of SHA-256, so their fakes differ
from other versions' fakes but are still consistent.

**Limits.** Masking covers table columns. It doesn't rewrite PostgreSQL large objects, and
it can't mask values that are only embedded inside JSON documents or free text. Use `redact`
or `null` on those columns, or empty their tables.
