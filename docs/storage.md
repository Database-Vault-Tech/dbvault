# Storage

DBVault writes backups through one interface:

```go
type Storage interface {
    Upload(ctx context.Context, key string, r io.Reader) (int64, error)
    Download(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
    Stat(ctx context.Context, key string) (Object, error)
    List(ctx context.Context, prefix string) ([]Object, error)
    Describe() string
}
```

Two implementations back four destination types:

- **S3-compatible** (`internal/storage/s3.go`) for Amazon S3, Cloudflare R2 and MinIO.
  Streams of unknown length are uploaded with multipart uploads using one reusable part
  buffer (16 MiB parts for the first 32 GiB, then 64 MiB, then 256 MiB, which is enough for
  multi-terabyte dumps within S3's 10,000-part limit). A failed upload is aborted, so no
  partial object is ever left behind. Small backups use a single `PutObject`.
- **Local filesystem** (`internal/storage/local.go`) writes to a temp file, fsyncs, then
  renames atomically. Paths are confined to `LOCAL_STORAGE_ROOT`; traversal is rejected.

When you add or edit a destination, DBVault proves it works with a write → read → delete
round trip. Credentials are sealed with AES-256-GCM and are never returned by the API (only
a masked access key hint).

## Object layout

```
<prefix>/<database>/<YYYY>/<MM>/<DD>/backup_<YYYY-MM-DD_HH-MM-SS>.dump.<zst|gz>[.age]
```

For example `production/2026/09/19/backup_2026-09-19_12-30-00.dump.zst.age`. Timestamps are
UTC. If two backups of a database start in the same second, the second gets a short id
suffix.

## Amazon S3

1. Create a bucket (for example `dbvault-production` in `eu-west-1`). Enable default
   encryption and, if you like, versioning with a lifecycle rule for non-current versions.
2. Create an IAM user or role for DBVault with this policy:

   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       { "Effect": "Allow",
         "Action": ["s3:ListBucket", "s3:GetBucketLocation", "s3:ListBucketMultipartUploads"],
         "Resource": "arn:aws:s3:::dbvault-production" },
       { "Effect": "Allow",
         "Action": ["s3:PutObject", "s3:GetObject", "s3:DeleteObject",
                    "s3:AbortMultipartUpload", "s3:ListMultipartUploadParts"],
         "Resource": "arn:aws:s3:::dbvault-production/*" }
     ]
   }
   ```

3. In DBVault: *Storage → Add storage → Amazon S3*, with the bucket, region, access key and
   secret. Optionally set a prefix to share a bucket.

**Other S3-compatible services** (Backblaze B2, Wasabi, DigitalOcean Spaces, Ceph…): choose
Amazon S3 and set the custom endpoint.

Consider enabling S3 Object Lock (compliance mode) on the bucket for ransomware-resistant
backups. Retention deletes would then fail until the lock expires, which is the intent.

## Cloudflare R2

1. In the Cloudflare dashboard: *R2 → Create bucket*.
2. *R2 → Manage R2 API Tokens → Create API token* with **Object Read & Write** scoped to the
   bucket. Copy the Access Key ID and Secret Access Key.
3. In DBVault: *Storage → Add storage → Cloudflare R2* with your 32-character **account ID**
   (visible in the R2 overview), the bucket and the keys. The endpoint
   `https://<account-id>.r2.cloudflarestorage.com` is derived automatically. For EU
   jurisdiction buckets, set the endpoint explicitly
   (`https://<account-id>.eu.r2.cloudflarestorage.com`).

R2 has no egress fees, which makes frequent restore tests cheap.

## MinIO

Docker Compose runs MinIO and `minio-init` creates the `S3_BUCKET` bucket plus a service
account (`S3_ACCESS_KEY`) whose policy only allows that bucket. In DBVault, *Storage → Add
storage → Use built-in storage* configures it in one click.

For your own MinIO: choose MinIO, set the endpoint (for example `https://minio.internal:9000`),
bucket and keys. Path-style addressing is always used for MinIO. The MinIO console for the
bundled instance is at http://localhost:9001 (root credentials in `.env`).

## Local filesystem

Choose *Local filesystem* and a directory relative to `LOCAL_STORAGE_ROOT`
(`/var/lib/dbvault/backups`, the `dbvault-backups` volume in Docker Compose). The volume is
mounted into both the worker (writes) and the API (downloads/deletes). Local storage is
convenient for testing, but a backup on the same machine as the database is not a real
backup: prefer object storage for anything important, or back the volume up elsewhere.

## Deleting destinations

A destination that still has schedules or completed backups can't be deleted. Move the
schedules and delete (or let retention expire) the backups first, so nothing is ever
orphaned.
