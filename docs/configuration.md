# Configuration

DBVault is configured with environment variables. With Docker Compose they go in `.env`
next to `docker-compose.yml`; [`.env.example`](../.env.example) lists every one with a
comment. Run `docker compose up -d` after changing them.

## Application

| Variable | Default | Purpose |
|---|---|---|
| `APP_URL` | `http://localhost:3000` | Public URL of the dashboard. Used in email links and the CSRF origin check, so it must match what people type in their browser. |
| `APP_PORT` | `3000` | Host port the dashboard is published on. |
| `APP_BIND` | `0.0.0.0` | Address the dashboard listens on (production overlay). Use `127.0.0.1` behind a reverse proxy. |
| `ALLOW_REGISTRATION` | `true` | Let anyone who can reach the app sign up. The first account can always be created; set to `false` afterwards and invite people instead. |
| `INSTANCE_ADMIN_EMAILS` | — | Comma-separated emails that get the read-only *Instance admin* area listing every organization. |
| `LANDING_PAGE` | `false` | `true` shows the marketing page at `/`. Otherwise `/` goes straight to sign-in. |
| `COOKIE_SECURE` | auto | Mark cookies `Secure`. Defaults to on when `APP_URL` is `https://`. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |

## Secrets

| Variable | Purpose |
|---|---|
| `ENCRYPTION_KEY` | 32 random bytes, base64. The master key for stored credentials, two-factor secrets and every organization's backup key. **Back it up**: without it, encrypted backups can't be decrypted. |
| `AUTH_SECRET` | 32+ characters. Signs CSRF tokens and keys the hashes of session and API tokens. |

Leave both empty and DBVault generates them on first start in the `dbvault-secrets` volume.
`scripts/setup.sh` writes them to `.env` instead, which makes backing them up easier.
Generate your own with `openssl rand -base64 32` (and `48` for `AUTH_SECRET`).

## Databases DBVault can back up

| Variable | Default | Purpose |
|---|---|---|
| `SQLITE_HOST_DIR` | `./data/sqlite` | Folder on the host holding SQLite files, mounted at `/sqlite` (`SQLITE_ROOT`) in the api and worker. Must be readable and writable by uid 10001. See [Database engines](engines.md#sqlite). |
| `PG_BIN_DIR`, `MYSQL_BIN_DIR` | on `PATH` | Where the worker finds `pg_dump`/`pg_restore` and `mariadb-dump`/`mariadb` outside Docker. |
| `ALLOW_PRIVATE_NETWORK_TARGETS` | `true` | Allow databases and webhooks on private or loopback addresses. Set to `false` on multi-tenant installs. |

## Storage

| Variable | Purpose |
|---|---|
| `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_REGION` | The built-in bucket offered as one-click storage (the bundled MinIO in Compose). |
| `MINIO_ROOT_USER`, `MINIO_ROOT_PASSWORD`, `MINIO_CONSOLE_PORT` | The bundled MinIO server and its console. |
| `LOCAL_STORAGE_ROOT` | Folder that local-disk destinations live under (a Docker volume in Compose). |

Your own S3, R2 and MinIO buckets are added in the dashboard. See [Storage](storage.md).

## Workers and restore testing

| Variable | Default | Purpose |
|---|---|---|
| `WORKER_CONCURRENCY` | `2` | Jobs each worker runs in parallel. |
| `WORKER_SHUTDOWN_GRACE_PERIOD` | `5m` | How long a stopping worker lets running jobs finish. |
| `VERIFY_UPLOADED_DATA` | `true` | Read every upload back and re-check its checksum. |
| `VERIFY_MODE` | `server` | How restore tests run: `server` (bundled verification databases), `docker` (a container per test) or `disabled`. SQLite tests always run. See [Restore and verification](restore.md). |
| `VERIFY_POSTGRES_PASSWORD`, `VERIFY_MYSQL_PASSWORD` | — | Passwords of the bundled verification servers. |
| `DOCKER_GID` | `999` | Group owning the Docker socket, for `VERIFY_MODE=docker`. |

## Email

Notifications, invitations and password resets are sent by email. Locally everything goes
to the bundled Mailpit at http://localhost:8025.

| Variable | Purpose |
|---|---|
| `SMTP_HOST`, `SMTP_PORT` | Your mail provider, e.g. `smtp.postmarkapp.com` and `587`. |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | Credentials, if your provider needs them. |
| `SMTP_FROM` | Sender, e.g. `DBVault <backups@example.com>`. |
| `SMTP_TLS` | `starttls`, `tls` or `none`. |

Webhook notifications need no configuration: add them in the dashboard under
*Notifications*. Each request is signed with HMAC-SHA256 so you can check it came from
DBVault.

## Rate limits

| Variable | Default | Purpose |
|---|---|---|
| `RATE_LIMIT_AUTH_PER_MINUTE` | `10` | Sign-in and sign-up requests per minute per client. |
| `RATE_LIMIT_API_PER_MINUTE` | `600` | API requests per minute per client. |
