/**
 * Canonical site identity used for metadata, sitemap, robots and social
 * cards. Set SITE_URL (build and runtime) to your real domain.
 */
export const SITE_URL = (process.env.SITE_URL ?? process.env.NEXT_PUBLIC_SITE_URL ?? "https://dbvault.tech").replace(/\/$/, "")

export const SITE_NAME = "DBVault"

export const SITE_TAGLINE = "Open-source SQL database backups that just work"

export const SITE_DESCRIPTION =
  "Automated, encrypted, restore-tested backups for PostgreSQL, MySQL and MariaDB. Schedule dumps, store them in S3, R2, MinIO or on disk, verify every restore, and self-host the whole thing. Apache-2.0."

/** Terms people actually search for when they need this tool. */
export const SITE_KEYWORDS = [
  "database backup",
  "postgresql backup",
  "postgres backup tool",
  "mysql backup",
  "mariadb backup",
  "pg_dump automation",
  "automated database backups",
  "encrypted database backups",
  "database restore testing",
  "backup verification",
  "point in time backup schedule",
  "self-hosted backup software",
  "open source backup tool",
  "s3 database backup",
  "cloudflare r2 backup",
  "minio backup",
  "disaster recovery",
  "backup retention policy",
  "gfs retention",
  "docker database backup",
]
