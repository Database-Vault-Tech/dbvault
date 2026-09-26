# Getting started

DBVault runs on your own server with Docker. This guide takes you from nothing to a
verified, encrypted backup in about ten minutes.

## Install

Requirements: Docker with Compose v2.

```bash
git clone https://github.com/Database-Vault-Tech/dbvault && cd dbvault
./scripts/setup.sh            # writes .env with strong, random secrets
docker compose up -d
```

`scripts/setup.sh` copies `.env.example` to `.env` and generates the secrets for you. You can
also copy the file yourself and edit it; every setting is described in
[Configuration](configuration.md).

Open **http://localhost:3000** and create an account. The first account can always be
created; set `ALLOW_REGISTRATION=false` afterwards if nobody else should sign up without an
invitation.

> **Back up your `ENCRYPTION_KEY`.** It protects every stored credential and every backup
> key. Keep a copy somewhere other than the DBVault server, such as a password manager.

## Your first backup

The dashboard shows a checklist. Work through it once:

1. **Add a database.** Pick PostgreSQL, MySQL, MariaDB or SQLite. For the network engines,
   enter the host, port, credentials and SSL mode. For SQLite, enter the file's path inside
   the mounted SQLite folder. Click **Test Connection** to check it before saving.
   [Database engines](engines.md) covers each engine's requirements.
2. **Add storage.** Use the bundled MinIO with one click, or connect your own Amazon S3,
   Cloudflare R2, MinIO bucket or a local folder. See [Storage](storage.md).
3. **Create a schedule.** Choose how often to back up, how long to keep backups, and whether
   to run an automatic restore test after each one.
4. **Run a backup.** Watch the dump, compression, encryption, upload and checksum check live.

To try it on sample data first, start the demo databases and add one with host
`sample-postgres`, `sample-mysql` or `sample-mariadb`, database `shop`, user `shop` and
password `shop-password`:

```bash
docker compose -f docker-compose.yml -f docker/e2e.yml up -d
```

## Prove it restores

Open a completed backup and click **Verify backup**. DBVault downloads it, checks its
SHA-256 checksum, decrypts it, restores it into a disposable database, and counts every
table's rows. A backup only shows **Verified** when every step passed. See
[Restore and verification](restore.md).

## Secure your account

- Turn on **two-factor authentication** under *Settings → Security*, and save the recovery
  codes somewhere safe.
- As the organization owner, export the **backup recovery key** from the same page and
  store it offline. With it, backups can be decrypted with the standard `age` tool even if
  DBVault itself is lost.

[Security](security.md) explains how DBVault protects credentials, keys and backups.

## Next steps

- Invite your team under *Settings → Team* and give each person the least access they need.
- Set up [notifications](configuration.md#email) so failed backups reach you by email or
  webhook.
- Install the [CLI](cli.md) to run backups from scripts and CI.
- Moving to a real server? Follow [Deployment](deployment.md).
