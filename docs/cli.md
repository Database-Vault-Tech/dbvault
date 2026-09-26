# CLI

The `dbvault` command line tool runs backups, restores and checks from a terminal, a
script or CI. It talks to your DBVault server's API.

## Install

The CLI is a single Go binary. Build it from the repository (Go 1.26+):

```bash
git clone --depth 1 https://github.com/Database-Vault-Tech/dbvault.git
cd dbvault/cli && go build -o dbvault . && sudo mv dbvault /usr/local/bin/
```

## Sign in

```bash
dbvault init --server https://dbvault.example.com
```

`init` asks for your email and password and creates an API token for this machine. If your
account has two-factor authentication, it also asks for a code; pass `--code` to supply it
up front. To use a token you already have (from *Settings → Security → API tokens*):

```bash
dbvault init --server https://dbvault.example.com --token dbv_...
```

The config is saved to `~/.config/dbvault/config.json` with mode `0600`.

In CI, skip `init` and set environment variables instead:

| Variable | Purpose |
|---|---|
| `DBVAULT_SERVER` | Server URL. |
| `DBVAULT_TOKEN` | An API token. |
| `DBVAULT_ORG` | Organization id or slug, if you belong to more than one. |

## Commands

```text
dbvault status                               workers, restore testing, backup health
dbvault database list
dbvault database add                         add a database (prompts for what's missing)
dbvault database test <name>                 test the connection
dbvault database remove <name> [--yes]
dbvault backup <database>                    back up now, with live progress
dbvault backup list [--database name] [--limit 20]
dbvault backup verify <backup-id>            full restore test
dbvault restore <backup-id> --new-database <name>
dbvault restore <backup-id> --existing       overwrite (asks you to type RESTORE)
dbvault storage list
dbvault schedule list
dbvault version
```

Every command accepts `--json` for machine-readable output, `--server` and `--org` to
override the saved config, and `--no-color`.

### Adding databases

```bash
# PostgreSQL, MySQL or MariaDB from a connection string (the password is prompted for)
dbvault database add --name production --url "postgres://app@db.internal:5432/shop?sslmode=require"

# Password from stdin, for scripts
echo "$PGPASSWORD" | dbvault database add --name production --host db.internal \
  --database shop --username app --password-stdin

# SQLite: a path inside the server's SQLite folder
dbvault database add --name blog --file myapp/app.db
```

Passwords are never accepted as flags, so they don't end up in shell history or process
listings.

### Backups

```bash
dbvault backup production                    # waits and shows progress
dbvault backup production --detach           # queue it and exit
dbvault backup production --storage s3-eu --compression gzip
```

Manual backups are never removed by retention policies.

### Restores

```bash
dbvault restore 3f2a… --new-database shop_restored          # safe: a new database
dbvault restore 3f2a… --new-database myapp/restored.db      # SQLite: a new file
dbvault restore 3f2a… --existing --confirm RESTORE          # overwrite, non-interactive
```

Use `--target <database>` to restore into a different database of the same engine.
