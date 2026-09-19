// Types mirroring the DBVault REST API (see backend/internal/*).

export type Role = "owner" | "admin" | "member" | "viewer"

export interface User {
  id: string
  email: string
  name: string
  last_login_at: string | null
  created_at: string
}

export interface Organization {
  id: string
  name: string
  slug: string
  role: Role
  created_at: string
}

export interface Me {
  user: User
  organizations: Organization[]
  auth_method: "user" | "api_token"
  csrf_token?: string
}

export type SSLMode = "disable" | "allow" | "prefer" | "require" | "verify-ca" | "verify-full"

export interface ServerInfo {
  version: string
  version_num: number
  major: number
  size_bytes: number
  table_count: number
  full_version: string
  current_user: string
  is_superuser: boolean
  in_recovery: boolean
  latency_ms: number
}

export interface ConnectionTest {
  ok: boolean
  message: string
  server?: ServerInfo
  tested_at: string
}

export type DatabaseEngine = "postgres" | "mysql" | "mariadb" | "sqlserver" | "sqlite"

export interface Database {
  id: string
  organization_id: string
  name: string
  engine: DatabaseEngine
  host: string
  port: number
  database: string
  username: string
  ssl_mode: SSLMode
  has_ssl_root_cert: boolean
  pg_version: string | null
  size_bytes: number | null
  last_tested_at: string | null
  last_test_ok: boolean | null
  last_test_error: string | null
  created_at: string
  updated_at: string
  protected: boolean
  schedule_count: number
  backup_count: number
  last_backup_at: string | null
  last_backup_status: BackupStatus | null
  last_success_at: string | null
  next_run_at: string | null
  storage_bytes: number
}

export type HealthStatus = "healthy" | "warning" | "critical" | "unprotected"

export interface DatabaseHealth {
  status: HealthStatus
  reason: string
  completed_30d: number
  failed_30d: number
  success_rate_30d: number | null
  last_failure_at: string | null
  last_verified_at: string | null
}

export interface DatabaseInput {
  name: string
  host: string
  port: number
  database: string
  username: string
  password?: string
  ssl_mode: SSLMode
  ssl_root_cert?: string
}

export type StorageType = "local" | "s3" | "r2" | "minio"

export interface StorageConfig {
  bucket?: string
  region?: string
  endpoint?: string
  account_id?: string
  force_path_style?: boolean
  prefix?: string
  path?: string
}

export interface StorageDestination {
  id: string
  name: string
  type: StorageType
  config: StorageConfig
  access_key_hint?: string
  is_default: boolean
  last_tested_at: string | null
  last_test_ok: boolean | null
  last_test_error: string | null
  created_at: string
  updated_at: string
  backup_count: number
  used_bytes: number
  schedule_count: number
  location: string
}

export interface StorageInput {
  name: string
  type: StorageType
  config: StorageConfig
  access_key_id?: string
  secret_access_key?: string
  is_default?: boolean
  use_builtin?: boolean
}

export interface StorageTest {
  ok: boolean
  message: string
  location: string
  tested_at: string
}

export interface BuiltinStorage {
  available: boolean
  type?: "minio"
  endpoint?: string
  bucket?: string
  console_url?: string
}

export interface RetentionPolicy {
  daily: number
  weekly: number
  monthly: number
}

export type SchedulePreset = "hourly" | "every_6_hours" | "daily" | "weekly" | "custom"
export type Compression = "zstd" | "gzip" | "none"

export interface Schedule {
  id: string
  database_id: string
  database_name: string
  storage_destination_id: string
  storage_name: string
  storage_type: StorageType
  name: string
  preset: SchedulePreset
  cron_expression: string
  timezone: string
  description: string
  enabled: boolean
  compression: Compression
  encryption: boolean
  retention: RetentionPolicy
  verify_after_backup: boolean
  next_run_at: string | null
  last_run_at: string | null
  last_backup_status: BackupStatus | null
  backup_count: number
  created_at: string
  updated_at: string
}

export interface ScheduleInput {
  database_id: string
  storage_destination_id: string
  name?: string
  preset: SchedulePreset
  cron_expression?: string
  timezone: string
  enabled?: boolean
  compression: Compression
  encryption?: boolean
  retention: RetentionPolicy
  verify_after_backup: boolean
}

export interface SchedulePreview {
  valid: boolean
  error?: string
  cron_expression?: string
  description?: string
  next_runs?: string[]
}

export interface RetentionDecision {
  id: string
  created_at: string
  keep: boolean
  reasons: string[]
}

export interface RetentionPreview {
  policy: RetentionPolicy
  enabled: boolean
  summary: string
  keep: number
  delete: number
  decisions: RetentionDecision[]
}

export type BackupStatus = "queued" | "running" | "completed" | "failed" | "cancelled" | "deleted"
export type VerificationStatus = "none" | "running" | "passed" | "failed" | "unavailable"
export type CheckStatus = "pass" | "fail" | "skipped" | "unavailable"

export interface VerificationCheck {
  status: CheckStatus
  message: string
}

export interface VerificationReport {
  integrity: VerificationCheck
  restore: VerificationCheck
  database: VerificationCheck
  tables_expected: number
  tables_restored: number
  rows: number
  sandbox: string
  duration_ms: number
  started_at: string
  completed_at: string
}

export interface Backup {
  id: string
  organization_id: string
  database_id: string
  database_name: string
  engine: DatabaseEngine
  schedule_id: string | null
  schedule_name: string | null
  storage_destination_id: string
  storage_name: string
  storage_type: StorageType
  job_id: string | null
  trigger: "manual" | "scheduled"
  status: BackupStatus
  storage_key: string | null
  format: string
  compression: Compression
  encrypted: boolean
  encryption_key_id: string | null
  size_bytes: number | null
  raw_size_bytes: number | null
  checksum_sha256: string | null
  pg_version: string | null
  pg_dump_version: string | null
  table_count: number | null
  error: string | null
  started_at: string | null
  completed_at: string | null
  duration_ms: number | null
  verification_status: VerificationStatus
  verification: VerificationReport | null
  verified_at: string | null
  deleted_at: string | null
  deleted_reason: string | null
  created_by: string | null
  created_at: string
}

export type JobType = "backup" | "restore" | "verification" | "cleanup" | "notification"
export type JobStatus = "queued" | "running" | "completed" | "failed" | "cancelled"

export interface BackupProgress {
  phase: "dumping" | "verifying"
  bytes_dumped: number
  bytes_written: number
  estimated_total: number
}

export interface Job {
  id: string
  organization_id: string | null
  type: JobType
  status: JobStatus
  payload: Record<string, string>
  result: unknown
  progress: BackupProgress | null
  error: string | null
  attempts: number
  max_attempts: number
  cancel_requested: boolean
  worker_id: string | null
  heartbeat_at: string | null
  run_after: string
  started_at: string | null
  completed_at: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface LogEntry {
  id: number
  level: "debug" | "info" | "warn" | "error"
  message: string
  created_at: string
}

export interface BackupDetail {
  backup: Backup
  job?: Job
  logs?: LogEntry[]
  verification_job?: Job
  verification_logs?: LogEntry[]
}

export interface QueuedJob {
  backup_id: string
  job_id: string
  status: JobStatus
}

export type RestoreStatus = "queued" | "running" | "verifying" | "completed" | "failed" | "cancelled"

export interface RestoreJob {
  id: string
  job_id: string | null
  backup_id: string
  backup_created_at: string
  source_database_name: string
  target_database_id: string
  target_database_name: string
  mode: "existing" | "new"
  new_database_name: string | null
  status: RestoreStatus
  error: string | null
  verification: { tables_expected: number; tables_found: number; rows: number; missing?: string[] } | null
  started_at: string | null
  completed_at: string | null
  duration_ms: number | null
  requested_by: string | null
  requested_by_email: string | null
  created_at: string
}

export interface RestoreInput {
  backup_id: string
  target_database_id: string
  mode: "existing" | "new"
  new_database_name?: string
  confirmation?: string
}

export type NotificationEvent =
  | "backup.succeeded"
  | "backup.failed"
  | "restore.completed"
  | "restore.failed"
  | "verification.passed"
  | "verification.failed"
  | "storage.failed"

export interface NotificationChannel {
  id: string
  name: string
  type: "email" | "webhook"
  config: { recipients?: string[]; url_hint?: string }
  events: NotificationEvent[]
  enabled: boolean
  has_signing_secret: boolean
  created_at: string
  updated_at: string
  last_delivery_status: "pending" | "delivered" | "failed" | null
  last_delivery_at: string | null
}

export interface NotificationInput {
  name: string
  type: "email" | "webhook"
  recipients?: string[]
  url?: string
  events: NotificationEvent[]
  enabled?: boolean
}

export interface NotificationEventInfo {
  type: NotificationEvent
  label: string
  description: string
}

export interface NotificationDelivery {
  id: string
  notification_id: string
  channel_name: string
  channel_type: "email" | "webhook"
  event: string
  status: "pending" | "delivered" | "failed"
  attempts: number
  response_status: number | null
  error: string | null
  title: string
  delivered_at: string | null
  created_at: string
}

export interface AuditLog {
  id: string
  actor_type: "user" | "api_token" | "system"
  actor_id: string | null
  actor_email: string | null
  action: string
  resource_type: string | null
  resource_id: string | null
  metadata: Record<string, unknown>
  ip_address: string | null
  user_agent: string | null
  created_at: string
}

export interface Member {
  user_id: string
  email: string
  name: string
  role: Role
  last_login_at: string | null
  joined_at: string
}

export interface Invitation {
  id: string
  email: string
  role: Exclude<Role, "owner">
  invited_by: string | null
  expires_at: string
  accepted_at: string | null
  created_at: string
  url?: string
}

export interface Session {
  id: string
  ip_address: string | null
  user_agent: string | null
  created_at: string
  last_seen_at: string
  expires_at: string
  current: boolean
}

export interface ApiToken {
  id: string
  name: string
  prefix: string
  last_used_at: string | null
  expires_at: string | null
  created_at: string
}

export interface DayStat {
  date: string
  completed: number
  failed: number
  bytes: number
}

export interface DashboardStats {
  databases: number
  protected_databases: number
  backups_today: number
  storage_used_bytes: number
  failed_backups_7d: number
  last_successful_backup: string | null
  verified_backups_30d: number
  running_jobs: number
  daily: DayStat[]
}

export interface Dashboard {
  stats: DashboardStats
  recent_backups: Backup[]
}

export interface WorkerPresence {
  id: string
  hostname: string
  started_at: string
  seen_at: string
  concurrency: number
  active_jobs: number
  running: Record<string, JobType>
  capabilities: {
    pg_dump_version?: string | null
    verify_mode?: string
    verify_available?: boolean
    verify_detail?: string
  }
}

export interface SystemStatus {
  version: string
  workers: WorkerPresence[]
  workers_online: number
  verification: { available: boolean; mode: string; message: string }
  email_configured: boolean
  builtin_storage: boolean
  allow_registration: boolean
}

export interface ListMeta {
  limit: number
  next_before?: string
}
