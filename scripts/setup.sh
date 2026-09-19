#!/usr/bin/env bash
# Creates .env from .env.example with strong random secrets.
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ -f .env && "${1:-}" != "--force" ]]; then
  echo ".env already exists (use --force to overwrite it)." >&2
  exit 1
fi
command -v openssl >/dev/null || { echo "openssl is required" >&2; exit 1; }

rand() { openssl rand -base64 "$1" | tr -d '\n/+=' | cut -c1-"$2"; }

cp .env.example .env
set_var() {
  local key=$1 value=$2
  # Use a delimiter that can't appear in base64 output.
  sed -i.bak "s|^${key}=.*|${key}=${value}|" .env && rm -f .env.bak
}
set_var ENCRYPTION_KEY "$(openssl rand -base64 32)"
set_var AUTH_SECRET "$(openssl rand -base64 48 | tr -d '\n')"
set_var POSTGRES_PASSWORD "$(rand 32 32)"
set_var MINIO_ROOT_PASSWORD "$(rand 32 32)"
set_var S3_SECRET_KEY "$(rand 32 32)"
set_var VERIFY_POSTGRES_PASSWORD "$(rand 32 32)"
if [[ -S /var/run/docker.sock ]]; then
  set_var DOCKER_GID "$(stat -c %g /var/run/docker.sock 2>/dev/null || stat -f %g /var/run/docker.sock)"
fi
chmod 600 .env

cat <<MSG
Created .env with random secrets.

IMPORTANT: back up ENCRYPTION_KEY from .env somewhere safe (a password
manager or secrets vault). Without it, encrypted backups cannot be decrypted.

Start DBVault:  docker compose up -d
Then open:      $(grep ^APP_URL= .env | cut -d= -f2)
MSG
