#!/bin/sh
# Prepares MinIO for DBVault: creates the backup bucket and a service account
# restricted to that bucket. Idempotent: safe to run on every start.
set -eu

for i in $(seq 1 60); do
  if mc alias set local http://minio:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null 2>&1; then
    break
  fi
  echo "waiting for minio ($i)..."
  sleep 1
done

mc mb --ignore-existing "local/$S3_BUCKET"

cat > /tmp/dbvault-policy.json <<POLICY
{
  "Version": "2012-10-17",
  "Statement": [
    {"Effect": "Allow", "Action": ["s3:GetBucketLocation", "s3:ListBucket", "s3:ListBucketMultipartUploads"],
     "Resource": ["arn:aws:s3:::$S3_BUCKET"]},
    {"Effect": "Allow", "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:AbortMultipartUpload", "s3:ListMultipartUploadParts"],
     "Resource": ["arn:aws:s3:::$S3_BUCKET/*"]}
  ]
}
POLICY

mc admin policy create local dbvault-backups /tmp/dbvault-policy.json >/dev/null 2>&1 || true
if ! mc admin user info local "$S3_ACCESS_KEY" >/dev/null 2>&1; then
  mc admin user add local "$S3_ACCESS_KEY" "$S3_SECRET_KEY"
fi
mc admin policy attach local dbvault-backups --user "$S3_ACCESS_KEY" >/dev/null 2>&1 || true
echo "minio ready: bucket $S3_BUCKET, service account $S3_ACCESS_KEY"
