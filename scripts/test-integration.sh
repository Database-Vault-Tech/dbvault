#!/usr/bin/env bash
# Runs the backend integration tests against throwaway Docker containers:
# PostgreSQL 17 (source + metadata), Redis and MinIO. Requires Docker and the
# PostgreSQL 17+ client tools (pg_dump/pg_restore) on PATH.
set -euo pipefail
cd "$(dirname "$0")/../backend"

PREFIX=dbvault-it-$$
cleanup() { docker rm -f "$PREFIX-pg" "$PREFIX-redis" "$PREFIX-minio" >/dev/null 2>&1 || true; }
trap cleanup EXIT

# Throwaway services keep their data in tmpfs: faster, and nothing touches disk.
docker run -d --name "$PREFIX-pg" -e POSTGRES_PASSWORD=postgres --tmpfs /var/lib/postgresql/data:size=1g \
  -p 127.0.0.1::5432 postgres:17-alpine >/dev/null
docker run -d --name "$PREFIX-redis" --tmpfs /data -p 127.0.0.1::6379 redis:7-alpine >/dev/null
docker run -d --name "$PREFIX-minio" -e MINIO_ROOT_USER=minio -e MINIO_ROOT_PASSWORD=minio-secret --tmpfs /data:size=1g \
  -p 127.0.0.1::9000 minio/minio server /data >/dev/null

port() { docker port "$1" "$2" | head -1 | awk -F: '{print $NF}'; }
PG_PORT=$(port "$PREFIX-pg" 5432)
REDIS_PORT=$(port "$PREFIX-redis" 6379)
MINIO_PORT=$(port "$PREFIX-minio" 9000)

echo "waiting for postgres..."
for _ in $(seq 1 60); do
  docker exec "$PREFIX-pg" pg_isready -U postgres >/dev/null 2>&1 && break
  sleep 1
done
sleep 2
docker exec "$PREFIX-pg" psql -U postgres -qc "CREATE DATABASE dbvault_meta"
docker exec "$PREFIX-minio" sh -c 'mkdir -p /data/dbvault-test'

export DBVAULT_TEST_SOURCE_URL="postgres://postgres:postgres@127.0.0.1:${PG_PORT}/postgres?sslmode=disable"
export DBVAULT_TEST_DATABASE_URL="postgres://postgres:postgres@127.0.0.1:${PG_PORT}/dbvault_meta?sslmode=disable"
export DBVAULT_TEST_REDIS_URL="redis://127.0.0.1:${REDIS_PORT}/0"
export DBVAULT_TEST_S3_ENDPOINT="http://127.0.0.1:${MINIO_PORT}"
export DBVAULT_TEST_S3_BUCKET=dbvault-test
export DBVAULT_TEST_S3_ACCESS_KEY=minio
export DBVAULT_TEST_S3_SECRET_KEY=minio-secret

# Packages share one database and Redis, so run them one at a time.
go test -count=1 -race -p 1 "$@" ./...
