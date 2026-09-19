# syntax=docker/dockerfile:1
# DBVault backend: one image, three binaries (api, worker, scheduler).

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/dbvault/dbvault/backend/internal/app.Version=${VERSION}" \
      -o /out/ ./cmd/api ./cmd/worker ./cmd/scheduler \
 && mv /out/api /out/dbvault-api && mv /out/worker /out/dbvault-worker && mv /out/scheduler /out/dbvault-scheduler

FROM alpine:3.23
# postgresql18-client provides pg_dump/pg_restore 18, which can back up and
# restore every PostgreSQL server from 9.2 through 18. mariadb-client
# provides mariadb-dump/mariadb for MySQL 5.7+ and MariaDB 10+ (with
# mariadb-connector-c's caching_sha2_password plugin for MySQL 8+).
RUN apk add --no-cache ca-certificates tzdata postgresql18-client mariadb-client mariadb-connector-c \
 && addgroup -S -g 10001 dbvault && adduser -S -D -u 10001 -G dbvault -h /var/lib/dbvault dbvault \
 && mkdir -p /var/lib/dbvault/backups /var/lib/dbvault/work /var/lib/dbvault/secrets \
 && chown -R dbvault:dbvault /var/lib/dbvault && chmod 700 /var/lib/dbvault/secrets /var/lib/dbvault/work
COPY --from=build /out/ /usr/local/bin/
USER dbvault
ENV LOCAL_STORAGE_ROOT=/var/lib/dbvault/backups \
    WORK_DIR=/var/lib/dbvault/work
EXPOSE 8080 8081 8082
CMD ["dbvault-api"]
