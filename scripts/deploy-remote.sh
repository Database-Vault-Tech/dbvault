#!/usr/bin/env bash
# Rolls the DBVault stack over to a new set of images.
#
# Runs ON THE VPS: .github/workflows/deploy.yml pipes it in over SSH
# (`ssh host "... bash -s" < scripts/deploy-remote.sh`). It expects the
# compose files and a .env to already be in $DEPLOY_PATH.
#
# If the new images fail to come up healthy, the previously deployed tag is
# restored before the script exits non-zero.

set -euo pipefail

: "${DEPLOY_PATH:?DEPLOY_PATH is required}"
: "${DBVAULT_IMAGE_BACKEND:?DBVAULT_IMAGE_BACKEND is required}"
: "${DBVAULT_IMAGE_FRONTEND:?DBVAULT_IMAGE_FRONTEND is required}"
: "${DBVAULT_IMAGE_TAG:?DBVAULT_IMAGE_TAG is required}"
export DBVAULT_IMAGE_BACKEND DBVAULT_IMAGE_FRONTEND DBVAULT_IMAGE_TAG

WAIT_TIMEOUT=${WAIT_TIMEOUT:-600}

cd "$DEPLOY_PATH"

if [ ! -f .env ]; then
  echo "error: $DEPLOY_PATH/.env is missing." >&2
  echo "Create it on the server (see .env.example) or set the DEPLOY_ENV_FILE secret." >&2
  exit 1
fi

compose() { docker compose -f docker-compose.yml -f docker-compose.prod.yml "$@"; }

previous_tag=""
[ -f .deployed-tag ] && previous_tag=$(cat .deployed-tag)

echo "==> deploying ${DBVAULT_IMAGE_TAG} (previous: ${previous_tag:-none})"

compose pull --quiet

roll_back() {
  echo "==> rollout failed; last 80 log lines per service" >&2
  compose logs --tail 80 --no-color api worker scheduler frontend >&2 || true
  if [ -n "$previous_tag" ] && [ "$previous_tag" != "$DBVAULT_IMAGE_TAG" ]; then
    echo "==> rolling back to ${previous_tag}" >&2
    DBVAULT_IMAGE_TAG="$previous_tag" compose up -d --remove-orphans \
      --wait --wait-timeout "$WAIT_TIMEOUT" >&2 || echo "==> rollback also failed" >&2
  fi
  exit 1
}

compose up -d --remove-orphans --wait --wait-timeout "$WAIT_TIMEOUT" || roll_back

printf '%s' "$DBVAULT_IMAGE_TAG" > .deployed-tag

# Database migrations run on api startup, so a healthy api means they applied.
echo "==> deployed"
compose ps --format 'table {{.Service}}\t{{.Image}}\t{{.Status}}'

# Untagged layers left behind by the pull. Named images are kept so a manual
# rollback (DBVAULT_IMAGE_TAG=<old> docker compose up -d) stays instant.
#
# This host runs other projects' containers. `prune` never touches an image an
# existing container uses, and `until` keeps it off anything recent, so only
# genuinely orphaned layers go.
docker image prune -f --filter 'until=72h' >/dev/null 2>&1 || true
