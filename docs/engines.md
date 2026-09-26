# Database engines

DBVault backs up and restores **PostgreSQL**, **MySQL**, **MariaDB** and **SQLite**. Each engine is a
driver (`backend/internal/engine/<engine>`) that implements one small interface: connect and
inspect, stream a native dump, list the tables in a dump, restore, create/drop databases and
check restored tables. Everything else (compression, encryption, checksums, storage,
retention, schedules, notifications, verification reports) is shared and engine-agnostic.

| | PostgreSQL | MySQL | MariaDB | SQLite |
|---|---|---|---|---|
| Server versions | 9.2 – 18 | 5.7 – 9.x (tested: 5.7, 8.0, 8.4, 9) | 10.x – 11.x (tested: 10.6, 11.4, 11.8) | SQLite 3 files |
| Reached via | network | network | network | folder mounted into DBVault (`SQLITE_ROOT`) |
| Dump tool | `pg_dump --format=custom` | `mariadb-dump` | `mariadb-dump` | `VACUUM INTO` (built in, no binary needed) |
| Restore tool | `pg_restore` | `mariadb` | `mariadb` | write, integrity-check, rename |
| Artifact | `…dump.zst.age` | `…sql.zst.age` | `…sql.zst.age` | `…db.zst.age` (a plain SQLite file inside) |
| Consistency | snapshot (`pg_dump`) | `--single-transaction` (InnoDB) | `--single-transaction` (InnoDB) | snapshot (a read transaction) |
| Atomic restore | yes, one transaction | **no** (DDL commits implicitly) | **no** | yes, atomic file swap |
| Restore testing | sandbox container or server | sandbox container or server | sandbox container or server | always available: temporary file on the worker |
| Sandbox image (Docker mode) | `postgres:<major>-alpine` | `mysql:<major>` (`mysql:8` for 8.x) | `mariadb:<major>` | — |
| Verification server (server mode) | `VERIFY_POSTGRES_URL` | `VERIFY_MYSQL_URL` | `VERIFY_MARIADB_URL` | — |

The engine is chosen when a database is added and can't be changed afterwards. A backup can
only be restored into a database of the same engine.

## MySQL and MariaDB

### Tooling

The worker image ships the MariaDB client tools (`mariadb-dump`, `mariadb` and the
`caching_sha2_password` plugin), which work with both MySQL 5.7+ and MariaDB servers. Outside
Docker, install your distribution's MariaDB client and, if it's not on `PATH`, point
`MYSQL_BIN_DIR` at it. DBVault checks that the server really is the engine you picked
(MySQL vs MariaDB) and tells you which one to choose if not.

Backups run:

```
mariadb-dump --single-transaction --quick --routines --triggers --events \
  --hex-blob --no-tablespaces --default-character-set=utf8mb4 --max-allowed-packet=1G <database>
```

- `--single-transaction` takes a consistent snapshot of InnoDB tables without locking them.
  MyISAM and other non-transactional tables are not snapshotted.
- Routines, triggers and events are included; binary columns are hex-encoded.
- The dump contains no `CREATE DATABASE`, so it can be restored into any database name.
- Credentials are written to a private (`0600`) option file that is read with
  `--defaults-file` and deleted after the run. They never appear in the process list or the
  environment.
- `mariadb-dump` 11.4+ writes a "sandbox mode" first line that MySQL's own client rejects.
  DBVault strips it from stored dumps, so a downloaded, decrypted dump restores with either
  `mysql` or `mariadb`, and puts it back when restoring (it stops the dump from running shell
  commands).

### Recommended user

Backups only need read access to the database:

```sql
CREATE USER 'dbvault'@'%' IDENTIFIED BY '…';
GRANT SELECT, SHOW VIEW, TRIGGER, EVENT, LOCK TABLES ON app.* TO 'dbvault'@'%';
-- MySQL 8.0.20+: to include stored procedures owned by other users
GRANT SHOW_ROUTINE ON *.* TO 'dbvault'@'%';
```

### SSL modes

| DBVault | Meaning | MySQL equivalent |
|---|---|---|
| `disable` | No TLS | `DISABLED` |
| `prefer` (default) | TLS when the server supports it | `PREFERRED` |
| `require` | TLS required, certificate not verified | `REQUIRED` |
| `verify-full` | TLS, verify the CA (optionally your own PEM) and the hostname | `VERIFY_IDENTITY` |

Like libpq, `prefer` falls back to an unencrypted connection when the TLS handshake fails.
MySQL 5.7 servers usually offer only legacy TLS ciphers that modern clients refuse, so with
5.7 use `prefer` or `disable` on a trusted network (or upgrade the server's TLS setup).

### Restores

- **Not transactional.** MySQL commits DDL implicitly, so a restore that fails part-way
  leaves the target partially restored. The restore wizard says so and defaults to
  restoring into a **new database**; inspect it, then switch your application over.
- Restoring over an existing database drops and recreates each table in the dump
  (`DROP TABLE IF EXISTS`). Tables that aren't in the dump are left alone.
- **Definers.** Views, triggers, routines and events carry a `DEFINER` account from the
  source server that often doesn't exist, or has no rights, on the target. DBVault rewrites
  them to `CURRENT_USER` (the restoring user) and logs how many it changed, so restored
  views keep working.
- Restoring into a new database needs `CREATE` on the server and privileges on the new
  database. With binary logging on (the MySQL default), creating triggers and routines as a
  non-`SUPER` user also needs `log_bin_trust_function_creators=1`; DBVault's error message
  says so if you hit it.

### Restore testing

With `VERIFY_MODE=server` (the Compose default) DBVault creates `dbvault_verify_<random>` on
the bundled `verify-mysql` (MySQL 8.4) or `verify-mariadb` (MariaDB 11.4) service, restores
into it, counts every table's rows, and drops it. The verification server must be at least
as new as the backup's server (a MySQL 9 backup needs a MySQL 9 server, or
`VERIFY_MODE=docker`). In Docker mode a matching `mysql:<major>` or `mariadb:<major>`
container is started per test instead.

## SQLite

SQLite databases are files, not servers, so DBVault reaches them through a folder mounted
into its containers rather than over the network. This works when DBVault runs on the
same machine as the application (or shares a volume with it).

### Setup

1. Put the folder holding your database files in `.env` (the default is `./data/sqlite`
   next to `docker-compose.yml`):

   ```bash
   SQLITE_HOST_DIR=/srv/myapp/data
   ```

   Compose mounts it at `/sqlite` in the `api` and `worker` containers and sets
   `SQLITE_ROOT=/sqlite`. Outside Docker, set `SQLITE_ROOT` to the folder yourself. With
   `SQLITE_ROOT` unset, SQLite is disabled and the "Add database" form says how to enable it.
2. The containers run as **uid 10001**, which needs read and write access to the folder and
   its files: write access for restores, and because SQLite creates a `-shm` file next to
   WAL-mode databases even when only reading.

   ```bash
   sudo chown -R 10001 /srv/myapp/data        # or: a shared group with rw access
   ```
3. `docker compose up -d`, then **Add database → SQLite** and enter the file's path inside
   the folder, e.g. `app.db` or `tenants/acme.db`. The CLI equivalent is
   `dbvault database add --name blog --file app.db`.

Paths are always relative to the folder. Absolute paths, `..` and symlinks that lead
outside it are rejected, so DBVault can't be pointed at other files on the host. **Every
organization on the instance can reach every file in the folder**, so on a shared instance
mount only what you mean to protect.

### Backups

A backup runs `VACUUM INTO` against the live file through a read-only connection. That is
SQLite's own online snapshot: it reads inside a single transaction, so the copy is
consistent (committed changes, including ones still in the WAL, are in; uncommitted ones
aren't) while the application keeps reading and writing. The copy is also compacted. It
goes to the worker's private work folder, then streams through compression, encryption,
SHA-256 and upload like every other engine, and is deleted.

The stored artifact decrypts and decompresses to an ordinary SQLite database:

```bash
age -d -i recovery.key app-2026-09-26.db.zst.age | zstd -d > app.db
sqlite3 app.db .tables
```

### Restores

- DBVault writes the backup to a temporary file next to the target, runs SQLite's
  `quick_check` on it, and only then renames it over the target, so a restore either
  fully replaces the file or leaves it untouched.
- **Stop the application before restoring over its database.** DBVault refuses to replace
  a file whose `-wal` or `-journal` isn't empty (the database is open or wasn't closed
  cleanly), because SQLite would apply that stale log to the restored file. Restoring into
  a **new file** (the default) is always safe; point the application at it when ready.
- A replaced file keeps the original's permissions (and owner, when the worker is allowed
  to set it). New files get `0640` and the folder's owner where possible.

### Restore testing

Always available, with no Docker socket or verification server: the backup is restored
into a temporary folder on the worker, every table's rows are counted, SQLite's full
`integrity_check` runs, and the folder is deleted.
