# Development

## Prerequisites

- Go 1.26+
- Node.js 22+ (npm 10+)
- Docker with Compose v2
- PostgreSQL client tools (`pg_dump`, `pg_restore`), at least as new as the servers you back
  up. On Ubuntu: `sudo apt install postgresql-client-17` (via the PGDG repository).

## Run the infrastructure in Docker, the app on your machine

```bash
./scripts/setup.sh    # creates .env with random secrets
docker compose up -d postgres redis minio minio-init verify-postgres mailpit
```

The compose file doesn't publish PostgreSQL or Redis. For local development, publish them
with a small override (not committed):

```yaml
# docker-compose.override.yml
services:
  postgres:
    ports: ["127.0.0.1:5432:5432"]
  redis:
    ports: ["127.0.0.1:6379:6379"]
  minio:
    ports: ["127.0.0.1:9000:9000", "127.0.0.1:9001:9001"]
  verify-postgres:
    ports: ["127.0.0.1:5433:5432"]
```

Then export the backend environment (values from your `.env`):

```bash
set -a; source .env; set +a
export DBVAULT_ENV=development LOG_LEVEL=debug
export DATABASE_URL="postgres://dbvault:${POSTGRES_PASSWORD}@localhost:5432/dbvault?sslmode=disable"
export REDIS_URL=redis://localhost:6379/0
export S3_ENDPOINT=http://localhost:9000
export APP_URL=http://localhost:3000 API_ADDR=127.0.0.1:8080
export LOCAL_STORAGE_ROOT=$PWD/.data/backups WORK_DIR=$PWD/.data/work
export VERIFY_MODE=server VERIFY_POSTGRES_URL="postgres://postgres:${VERIFY_POSTGRES_PASSWORD}@localhost:5433/postgres?sslmode=disable"
export SMTP_HOST=localhost SMTP_PORT=1025
```

Run each process in its own terminal:

```bash
cd backend
go run ./cmd/api          # applies migrations, serves :8080
go run ./cmd/worker       # needs pg_dump/pg_restore (or PG_BIN_DIR) and mariadb-dump/mariadb (or MYSQL_BIN_DIR) on PATH
go run ./cmd/scheduler
```

And the dashboard with hot reload:

```bash
cd frontend
npm install
API_URL=http://localhost:8080 npm run dev     # http://localhost:3000
```

`VERIFY_MODE=docker` also works locally: the worker talks to your Docker daemon and starts
`postgres:<major>-alpine` containers with a published port on 127.0.0.1.

## Project conventions

- **Backend**: standard library first. `chi` for routing, `pgx` for PostgreSQL, SQL written
  by hand next to the code that uses it. Handlers validate input with `internal/validate`
  and return `apperr` errors; anything else becomes a generic 500 (details logged
  server-side only). Every mutation writes an audit entry.
- **Frontend**: App Router pages are thin server components rendering client components.
  Data access goes through `src/lib/api.ts` and the TanStack Query hooks in
  `src/lib/queries.ts`. Forms use react-hook-form + Zod. UI is shadcn/ui on Radix with the
  shared components in `src/components/app`.
- **Migrations** live in `backend/migrations/NNNN_description.sql`, are embedded in the
  binary and applied in order by the API on startup (serialized with an advisory lock).
  Never edit a released migration; add a new one.

## Tests

```bash
cd backend && go test ./...                 # unit tests
./scripts/test-integration.sh               # starts PostgreSQL/Redis/MinIO containers, runs everything
cd cli && go test ./...
cd frontend && npm run lint && npm run typecheck && npm test
```

Integration tests are skipped unless these are set (the script sets them):

| Variable | Used by |
|---|---|
| `DBVAULT_TEST_SOURCE_URL` | backup/restore engine tests (a server where tests may create databases) |
| `DBVAULT_TEST_DATABASE_URL`, `DBVAULT_TEST_REDIS_URL` | API and job queue tests |
| `DBVAULT_TEST_S3_ENDPOINT`, `…_BUCKET`, `…_ACCESS_KEY`, `…_SECRET_KEY` | S3/MinIO storage tests |

End-to-end:

```bash
docker compose -f docker-compose.yml -f docker/e2e.yml up -d --build --wait
cd frontend && npx playwright install chromium && E2E_BASE_URL=http://localhost:3000 npm run test:e2e
```

## Building

```bash
docker compose build                     # dbvault/backend and dbvault/frontend images
cd cli && go build -o dbvault .          # CLI
```

Set the version with `DBVAULT_VERSION` (images) or
`-ldflags "-X github.com/dbvault/dbvault/cli/cmd.Version=v1.2.3"` (CLI).

## Using an external PostgreSQL or Redis

Set `DATABASE_URL` / `REDIS_URL` on the `api`, `worker` and `scheduler` services (for
example in a `docker-compose.override.yml`) and remove the bundled services. DBVault's
metadata database needs the `citext` extension, which is trusted and can be created by the
database owner on PostgreSQL 13+.

## Backing up databases on the same machine

DBVault's API and worker run in containers, where `localhost` is the container itself.
To protect a PostgreSQL running on the Docker host (installed natively or published by
another container, e.g. `-p 5450:5432`), use host **`host.docker.internal`** with the
published port. The compose file maps that name to the host (`extra_hosts:
host-gateway`), so this works on Linux, macOS and Windows. The database must listen on an
address reachable from Docker (`0.0.0.0`, not only `127.0.0.1`).

For a database container on the same Docker network as DBVault, use its container
name and internal port (e.g. `my-postgres:5432`).
