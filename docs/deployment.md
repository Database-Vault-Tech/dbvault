# Deploying to a VPS

`.github/workflows/deploy.yml` builds the two images, pushes them to Docker
Hub and rolls the compose stack over on a single server via SSH.

```
push to main / tag v*        docker/build-push-action        ssh + compose
        │                            │                             │
        └──► build backend ──────────┴──► <namespace>/dbvault-backend:<tag>
             build frontend ─────────┴──► <namespace>/dbvault-frontend:<tag>
                                                   │
                                     scp docker-compose{,.prod}.yml + docker/
                                     compose pull && compose up -d --wait
                                     (rolls back to the previous tag on failure)
```

Images are tagged with the full commit SHA and with `latest`. The rollout
always pins the SHA, so `latest` moving never changes what is running, and any
past commit can be redeployed by its SHA.

## Server prerequisites

On the VPS, once:

```bash
curl -fsSL https://get.docker.com | sh          # Docker with Compose v2.24+
adduser deploy                                  # give it a long random password
usermod -aG docker deploy
install -d -o deploy -g deploy -m 755 /opt/dbvault
```

The workflow signs in with that password (`sshpass`), so the server must accept
password logins — in `/etc/ssh/sshd_config`:

```
PasswordAuthentication yes
PermitRootLogin no
```

then `systemctl restart ssh`. Use a long random password (`openssl rand -base64 24`)
that is used for nothing else, and install fail2ban. The workflow multiplexes
its SSH connections so a deploy authenticates once, not once per step.

> Password auth means the password sits in a GitHub secret and any host that
> can impersonate your server gets handed it. Pin the host key with
> `DEPLOY_KNOWN_HOSTS`, and switch to a key when you can — see
> [Switching to key auth](#switching-to-key-auth).

Grab the host key to pin, from your machine:

```bash
ssh-keyscan -p 22 your.server.ip 2>/dev/null   # → DEPLOY_KNOWN_HOSTS secret
```

Then create `/opt/dbvault/.env` — either by hand from `.env.example` (run
`./scripts/setup.sh` locally to generate strong secrets), or by putting the
whole file into the `DEPLOY_ENV_FILE` secret and letting the workflow write it.

Keep your filled-in copy at `.env.prod` in the repo checkout. It is gitignored,
so it is the one place to edit production config: change it, re-paste it into
the `DEPLOY_ENV_FILE` secret, and the next deploy picks it up. Never hand-edit
`/opt/dbvault/.env` on the server — the next rollout overwrites it.

For a real deployment, at minimum set in `.env`:

```ini
APP_URL=https://dbvault.example.com   # must match what users type
SITE_URL=https://dbvault.example.com
APP_BIND=127.0.0.1                    # if a reverse proxy terminates TLS
ENCRYPTION_KEY=...                    # openssl rand -base64 32 — BACK THIS UP
AUTH_SECRET=...                       # openssl rand -base64 48
POSTGRES_PASSWORD=...
MINIO_ROOT_PASSWORD=...
S3_SECRET_KEY=...
VERIFY_POSTGRES_PASSWORD=...
VERIFY_MYSQL_PASSWORD=...
ALLOW_REGISTRATION=false              # after you create the first account
```

> Losing `ENCRYPTION_KEY` makes every stored credential and every encrypted
> backup unrecoverable. Keep a copy outside the server.

## GitHub secrets

Repository → Settings → Secrets and variables → Actions → **Secrets**.

| Secret | Required | What it is |
| --- | --- | --- |
| `DOCKERHUB_TOKEN` | yes | Docker Hub access token with **Read & Write** scope (Account Settings → Personal access tokens). Not your password. Create the `dbvault-backend` and `dbvault-frontend` repositories on Docker Hub first — a token can push to an existing repository without being able to create one. |
| `DEPLOY_HOST` | yes | VPS hostname or IP. |
| `DEPLOY_USER` | yes | SSH user on the VPS, in the `docker` group (e.g. `deploy`). |
| `DEPLOY_SSH_PASSWORD` | yes | Password of that SSH user. Fed to `ssh`/`scp` through `sshpass -e`, so it never reaches a command line or the process list. |
| `DEPLOY_PORT` | no | SSH port, if not `22`. |
| `DEPLOY_KNOWN_HOSTS` | no | `ssh-keyscan` output for the server. Strongly recommended with password auth — without it the workflow trusts whatever host key it is handed, and hands that host the password. |
| `DEPLOY_ENV_FILE` | no | Full contents of the server's `.env`. When set, it is rewritten (mode 600) on every deploy; when unset, the `.env` already on the server is left alone. |

## GitHub variables

Same page, **Variables** tab. None are required.

| Variable | Default | What it does |
| --- | --- | --- |
| `DOCKERHUB_USERNAME` | — | **Required.** Docker Hub account, and the image namespace: `<user>/dbvault-backend`. It is a variable rather than a secret on purpose — GitHub blanks out any value containing a secret when it crosses between jobs, which would leave the image nameless. |
| `SITE_URL` | `https://dbvault.tech` | Baked into the frontend image at build time (canonical links, sitemap, robots, social cards). |
| `APP_URL` | — | Public URL. When set, the workflow smoke-tests `$APP_URL/api/health` after the rollout and shows the link on the run. |
| `DEPLOY_PATH` | `/opt/dbvault` | Directory on the VPS holding the compose files and `.env`. |
| `DEPLOY_PLATFORMS` | `linux/amd64` | Set to `linux/arm64` for an Ampere/Graviton VPS. |

## Gating rollouts behind an approval

The deploy job uses the `production` environment. Create it under Settings →
Environments and add required reviewers to make every rollout wait for a
manual approval.

## Running it

- Push to `main` — builds both images (tagged with the commit SHA and
  `latest`) and deploys the SHA tag.
- Actions → Deploy → **Run workflow** with `deploy_tag` set to an earlier
  commit SHA — redeploys that already-published tag without rebuilding. This
  is the fastest rollback.

## Rolling back by hand

On the server:

```bash
cd /opt/dbvault
DBVAULT_IMAGE_BACKEND=<namespace>/dbvault-backend \
DBVAULT_IMAGE_FRONTEND=<namespace>/dbvault-frontend \
DBVAULT_IMAGE_TAG=sha-1234567 \
  docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --wait
```

`cat /opt/dbvault/.deployed-tag` shows what the last successful rollout pinned.

## Switching to key auth

Password auth is the weaker option; moving to a key is a five-minute change:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/dbvault_deploy -C "github-actions" -N ""
ssh-copy-id -i ~/.ssh/dbvault_deploy.pub deploy@your.server.ip
```

Then put the private half (`cat ~/.ssh/dbvault_deploy`, the whole block
including the `BEGIN`/`END` lines) in a `DEPLOY_SSH_KEY` secret, and in the
`Configure SSH` step of `.github/workflows/deploy.yml`:

- write it to `~/.ssh/id_deploy` (mode 600) instead of installing `sshpass`,
- in the `Host dbvault` block, swap `PubkeyAuthentication no` /
  `PreferredAuthentications password,keyboard-interactive` for
  `IdentityFile ~/.ssh/id_deploy` and `IdentitiesOnly yes`,
- drop the `sshpass -e ` prefix from the four `ssh`/`scp` calls.

Finally set `PasswordAuthentication no` on the server and delete the
`DEPLOY_SSH_PASSWORD` secret.

## Behind a reverse proxy

Set `APP_BIND=127.0.0.1` in `.env` so only the proxy can reach the app, then
terminate TLS in front of it. With Caddy:

```
dbvault.example.com {
    reverse_proxy 127.0.0.1:3000
}
```

Keep `APP_URL` and `SITE_URL` on the `https://` URL — `APP_URL` feeds the CSRF
origin check and `COOKIE_SECURE` defaults to true when it starts with `https`.
