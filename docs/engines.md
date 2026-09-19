# Database engines

DBVault backs up and restores **PostgreSQL**, **MySQL** and **MariaDB**. Each engine is a
driver (`backend/internal/engine/<engine>`) that implements one small interface: connect and
inspect, stream a native dump, list the tables in a dump, restore, create/drop databases and
check restored tables. Everything else (compression, encryption, checksums, storage,
retention, schedules, notifications, verification reports) is shared and engine-agnostic.

| | PostgreSQL | MySQL | MariaDB |
|---|---|---|---|
| Server versions | 9.2 – 18 | 5.7 – 9.x (tested: 5.7, 8.0, 8.4, 9) | 10.x – 11.x (tested: 10.6, 11.4, 11.8) |
| Dump tool | `pg_dump --format=custom` | `mariadb-dump` | `mariadb-dump` |
| Restore tool | `pg_restore` | `mariadb` | `mariadb` |
| Artifact | `…dump.zst.age` | `…sql.zst.age` | `…sql.zst.age` |
| Consistency | snapshot (`pg_dump`) | `--single-transaction` (InnoDB) | `--single-transaction` (InnoDB) |
| Atomic restore | yes, one transaction | **no** (DDL commits implicitly) | **no** |
| Sandbox image (Docker mode) | `postgres:<major>-alpine` | `mysql:<major>` (`mysql:8` for 8.x) | `mariadb:<major>` |
| Verification server (server mode) | `VERIFY_POSTGRES_URL` | `VERIFY_MYSQL_URL` | `VERIFY_MARIADB_URL` |

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
