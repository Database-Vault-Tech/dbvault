#!/usr/bin/env bash
# Runs the backend integration tests against throwaway Docker containers:
# PostgreSQL 17 (source + metadata), MySQL 8.4, MariaDB 11.4, Redis and MinIO.
# Requires Docker and the PostgreSQL 17+ client tools (pg_dump/pg_restore) on
# PATH. The MySQL/MariaDB tests need the MariaDB client tools; without them
# on PATH they run inside a small Alpine container instead.
set -euo pipefail
cd "$(dirname "$0")/../backend"

PREFIX=dbvault-it-$$
cleanup() { docker rm -f "$PREFIX-pg" "$PREFIX-mysql" "$PREFIX-mariadb" "$PREFIX-redis" "$PREFIX-minio" >/dev/null 2>&1 || true; }
trap cleanup EXIT

# Throwaway services keep their data in tmpfs: faster, and nothing touches disk.
docker run -d --name "$PREFIX-pg" -e POSTGRES_PASSWORD=postgres --tmpfs /var/lib/postgresql/data:size=1g \
  -p 127.0.0.1::5432 postgres:17-alpine >/dev/null
docker run -d --name "$PREFIX-mysql" -e MYSQL_ROOT_PASSWORD=root --tmpfs /var/lib/mysql:size=1g \
  -p 127.0.0.1::3306 mysql:8.4 >/dev/null
docker run -d --name "$PREFIX-mariadb" -e MARIADB_ROOT_PASSWORD=root --tmpfs /var/lib/mysql:size=1g \
  -p 127.0.0.1::3306 mariadb:11.4 >/dev/null
docker run -d --name "$PREFIX-redis" --tmpfs /data -p 127.0.0.1::6379 redis:7-alpine >/dev/null
docker run -d --name "$PREFIX-minio" -e MINIO_ROOT_USER=minio -e MINIO_ROOT_PASSWORD=minio-secret --tmpfs /data:size=1g \
  -p 127.0.0.1::9000 quay.io/minio/minio server /data >/dev/null

port() { docker port "$1" "$2" | head -1 | awk -F: '{print $NF}'; }
PG_PORT=$(port "$PREFIX-pg" 5432)
REDIS_PORT=$(port "$PREFIX-redis" 6379)
MINIO_PORT=$(port "$PREFIX-minio" 9000)
MYSQL_PORT=$(port "$PREFIX-mysql" 3306)
MARIADB_PORT=$(port "$PREFIX-mariadb" 3306)

echo "waiting for postgres..."
for _ in $(seq 1 60); do
  docker exec "$PREFIX-pg" pg_isready -U postgres >/dev/null 2>&1 && break
  sleep 1
done
sleep 2
docker exec "$PREFIX-pg" psql -U postgres -qc "CREATE DATABASE dbvault_meta"

# The MySQL images run a temporary socket-only server while initialising, so
# wait for a TCP connection.
echo "waiting for mysql and mariadb..."
for _ in $(seq 1 90); do
  docker exec "$PREFIX-mysql" mysql -h127.0.0.1 -uroot -proot -e 'SELECT 1' >/dev/null 2>&1 &&
    docker exec "$PREFIX-mariadb" mariadb -h127.0.0.1 -uroot -proot -e 'SELECT 1' >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$PREFIX-minio" sh -c 'mkdir -p /data/dbvault-test'

export DBVAULT_TEST_SOURCE_URL="postgres://postgres:postgres@127.0.0.1:${PG_PORT}/postgres?sslmode=disable"
export DBVAULT_TEST_DATABASE_URL="postgres://postgres:postgres@127.0.0.1:${PG_PORT}/dbvault_meta?sslmode=disable"
export DBVAULT_TEST_REDIS_URL="redis://127.0.0.1:${REDIS_PORT}/0"
export DBVAULT_TEST_S3_ENDPOINT="http://127.0.0.1:${MINIO_PORT}"
export DBVAULT_TEST_S3_BUCKET=dbvault-test
export DBVAULT_TEST_S3_ACCESS_KEY=minio
export DBVAULT_TEST_S3_SECRET_KEY=minio-secret
export DBVAULT_TEST_MYSQL_URL="mysql://root:root@127.0.0.1:${MYSQL_PORT}/"
export DBVAULT_TEST_MARIADB_URL="mariadb://root:root@127.0.0.1:${MARIADB_PORT}/"

# Packages share one database and Redis, so run them one at a time.
go test -count=1 -race -p 1 "$@" ./...

if ! command -v mariadb-dump >/dev/null 2>&1; then
  echo "mariadb-dump not on PATH: running the MySQL/MariaDB tests in a container..."
  bin=$(mktemp -d)
  trap 'cleanup; rm -rf "$bin"' EXIT
  CGO_ENABLED=0 go test -c -o "$bin/integration.test" ./internal/integration
  printf 'FROM alpine:3.23\nRUN apk add --no-cache mariadb-client mariadb-connector-c\n' |
    docker build -q -t dbvault-it-mysql-tools - >/dev/null
  docker run --rm --network host -v "$bin:/t:ro" \
    -e DBVAULT_TEST_MYSQL_URL -e DBVAULT_TEST_MARIADB_URL \
    dbvault-it-mysql-tools /t/integration.test -test.count=1 -test.v -test.run 'MySQL'
fi
