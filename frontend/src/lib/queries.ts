"use client"

// TanStack Query hooks for every DBVault resource. Long-running work is
// tracked by polling the job/backup/restore while it is active.

import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query"

import { api } from "./api"
import type {
  AdminOrgDetail,
  AdminOrgSummary,
  AdminOverview,
  AdminUser,
  ApiToken,
  AuditLog,
  Backup,
  BackupDetail,
  BuiltinStorage,
  ConnectionTest,
  Dashboard,
  Database,
  DatabaseHealth,
  DatabaseInput,
  Invitation,
  Job,
  LogEntry,
  Me,
  Member,
  NotificationChannel,
  NotificationDelivery,
  NotificationEventInfo,
  NotificationInput,
  Organization,
  QueuedJob,
  RestoreInput,
  RestoreJob,
  RetentionPreview,
  Role,
  Schedule,
  ScheduleInput,
  SchedulePreview,
  Session,
  StorageDestination,
  StorageInput,
  StorageTest,
  SystemStatus,
} from "./types"

/** Builds a filter query string, dropping empty values, plus the cursor. */
function searchParams(filters: Record<string, string | undefined | null>, before?: string) {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(filters)) if (v) p.set(k, v)
  if (before) p.set("before", before)
  return p.toString()
}

export const keys = {
  me: ["me"] as const,
  admin: ["admin"] as const,
  adminOrg: (id: string) => ["admin", "organization", id] as const,
  system: ["system"] as const,
  dashboard: ["dashboard"] as const,
  databases: ["databases"] as const,
  database: (id: string) => ["databases", id] as const,
  storage: ["storage"] as const,
  builtinStorage: ["storage", "builtin"] as const,
  schedules: (databaseId?: string) => ["schedules", { databaseId }] as const,
  schedule: (id: string) => ["schedule", id] as const,
  schedulePreview: (preset: string, cron: string, tz: string) => ["schedule-preview", preset, cron, tz] as const,
  retention: (id: string) => ["schedule", id, "retention"] as const,
  backups: (filters: BackupFilters) => ["backups", filters] as const,
  backup: (id: string) => ["backup", id] as const,
  job: (id: string) => ["job", id] as const,
  restores: (filters: RestoreFilters) => ["restores", filters] as const,
  restore: (id: string) => ["restore", id] as const,
  notifications: ["notifications"] as const,
  notificationEvents: ["notifications", "events"] as const,
  deliveries: (filters: DeliveryFilters) => ["notifications", "deliveries", filters] as const,
  audit: (filters: AuditFilters) => ["audit", filters] as const,
  organization: ["organization"] as const,
  members: ["team", "members"] as const,
  invitations: ["team", "invitations"] as const,
  sessions: ["sessions"] as const,
  tokens: ["tokens"] as const,
}

const ACTIVE_JOB = new Set(["queued", "running"])
const ACTIVE_RESTORE = new Set(["queued", "running", "verifying"])

function invalidateBackupViews(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: ["backups"] })
  void qc.invalidateQueries({ queryKey: ["backup"] })
  void qc.invalidateQueries({ queryKey: keys.dashboard })
  void qc.invalidateQueries({ queryKey: keys.databases })
  void qc.invalidateQueries({ queryKey: ["schedules"] })
}

// ---------------------------------------------------------------- session

export function useMe() {
  return useQuery({
    queryKey: keys.me,
    queryFn: () => api.get<Me>("/me", { noOrg: true }),
    staleTime: 60_000,
    retry: false,
  })
}

// Instance admin: account-level (no org header), read-only.

export function useAdminOverview() {
  return useQuery({ queryKey: [...keys.admin, "overview"], queryFn: () => api.get<AdminOverview>("/admin/overview", { noOrg: true }), refetchInterval: 30_000 })
}

export function useAdminOrganizations() {
  return useQuery({ queryKey: [...keys.admin, "organizations"], queryFn: () => api.get<AdminOrgSummary[]>("/admin/organizations", { noOrg: true }) })
}

export function useAdminOrganization(id: string) {
  return useQuery({ queryKey: keys.adminOrg(id), queryFn: () => api.get<AdminOrgDetail>(`/admin/organizations/${id}`, { noOrg: true }) })
}

export function useAdminUsers() {
  return useQuery({ queryKey: [...keys.admin, "users"], queryFn: () => api.get<AdminUser[]>("/admin/users", { noOrg: true }) })
}

export function useSystemStatus() {
  return useQuery({
    queryKey: keys.system,
    queryFn: () => api.get<SystemStatus>("/system/status", { noOrg: true }),
    refetchInterval: 20_000,
    staleTime: 10_000,
  })
}

export function useDashboard() {
  return useQuery({
    queryKey: keys.dashboard,
    queryFn: () => api.get<Dashboard>("/dashboard"),
    refetchInterval: (q) => (q.state.data?.stats.running_jobs ? 3000 : 30_000),
  })
}

// -------------------------------------------------------------- databases

export function useDatabases() {
  return useQuery({ queryKey: keys.databases, queryFn: () => api.get<Database[]>("/databases") })
}

export function useDatabase(id: string) {
  return useQuery({
    queryKey: keys.database(id),
    queryFn: () => api.get<{ database: Database; health: DatabaseHealth }>(`/databases/${id}`),
    enabled: !!id,
  })
}

export function useCreateDatabase() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: DatabaseInput) => api.post<{ database: Database; test: ConnectionTest }>("/databases", input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.databases })
      void qc.invalidateQueries({ queryKey: keys.dashboard })
    },
  })
}

export function useUpdateDatabase(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: DatabaseInput) => api.patch<Database>(`/databases/${id}`, input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.databases }),
  })
}

export function useDeleteDatabase() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/databases/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.databases })
      void qc.invalidateQueries({ queryKey: keys.dashboard })
      void qc.invalidateQueries({ queryKey: ["schedules"] })
    },
  })
}

/** Test an unsaved connection (database_id lets edits reuse the stored password). */
export function useTestConnection() {
  return useMutation({
    mutationFn: (input: DatabaseInput & { database_id?: string }) => api.post<ConnectionTest>("/databases/test", input),
  })
}

export function useTestSavedDatabase() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<ConnectionTest>(`/databases/${id}/test`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.databases }),
  })
}

// ---------------------------------------------------------------- storage

export function useStorage() {
  return useQuery({ queryKey: keys.storage, queryFn: () => api.get<StorageDestination[]>("/storage") })
}

export function useBuiltinStorage() {
  return useQuery({ queryKey: keys.builtinStorage, queryFn: () => api.get<BuiltinStorage>("/storage/builtin"), staleTime: 300_000 })
}

export function useCreateStorage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: StorageInput) => api.post<{ storage: StorageDestination; test: StorageTest }>("/storage", input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.storage }),
  })
}

export function useUpdateStorage(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: StorageInput) => api.patch<StorageDestination>(`/storage/${id}`, input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.storage }),
  })
}

export function useDeleteStorage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/storage/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.storage }),
  })
}

export function useTestStorage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<StorageTest>(`/storage/${id}/test`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.storage }),
  })
}

export function useTestStorageInput() {
  return useMutation({
    mutationFn: (input: StorageInput & { storage_id?: string }) => api.post<StorageTest>("/storage/test", input),
  })
}

// -------------------------------------------------------------- schedules

export function useSchedules(databaseId?: string) {
  return useQuery({
    queryKey: keys.schedules(databaseId),
    queryFn: () => api.get<Schedule[]>(`/schedules${databaseId ? `?database_id=${databaseId}` : ""}`),
  })
}

export function useSchedule(id: string) {
  return useQuery({
    queryKey: keys.schedule(id),
    queryFn: () => api.get<{ schedule: Schedule; next_runs: string[] }>(`/schedules/${id}`),
    enabled: !!id,
  })
}

export function useSchedulePreview(preset: string, cron: string, timezone: string, enabled = true) {
  const params = new URLSearchParams({ preset, cron, timezone })
  return useQuery({
    queryKey: keys.schedulePreview(preset, cron, timezone),
    queryFn: () => api.get<SchedulePreview>(`/schedules/preview?${params}`),
    enabled,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  })
}

export function useRetentionPreview(id: string) {
  return useQuery({
    queryKey: keys.retention(id),
    queryFn: () => api.get<RetentionPreview>(`/schedules/${id}/retention`),
    enabled: !!id,
  })
}

export function useCreateSchedule() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: ScheduleInput) => api.post<Schedule>("/schedules", input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["schedules"] })
      void qc.invalidateQueries({ queryKey: keys.databases })
      void qc.invalidateQueries({ queryKey: keys.dashboard })
    },
  })
}

export function useUpdateSchedule(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: ScheduleInput) => api.patch<Schedule>(`/schedules/${id}`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["schedules"] })
      void qc.invalidateQueries({ queryKey: ["schedule"] })
      void qc.invalidateQueries({ queryKey: keys.databases })
    },
  })
}

export function useDeleteSchedule() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/schedules/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["schedules"] })
      void qc.invalidateQueries({ queryKey: keys.databases })
      void qc.invalidateQueries({ queryKey: keys.dashboard })
    },
  })
}

export function useRunSchedule() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<QueuedJob>(`/schedules/${id}/run`),
    onSuccess: () => invalidateBackupViews(qc),
  })
}

// ---------------------------------------------------------------- backups

export interface BackupFilters {
  database_id?: string
  schedule_id?: string
  status?: string
  trigger?: string
  limit?: number
}

function backupQuery(filters: BackupFilters, before?: string) {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(filters)) if (v !== undefined && v !== "") p.set(k, String(v))
  if (before) p.set("before", before)
  return `/backups?${p}`
}

export function useBackups(filters: BackupFilters = {}) {
  return useInfiniteQuery({
    queryKey: keys.backups(filters),
    queryFn: ({ pageParam }) => api.list<Backup>(backupQuery(filters, pageParam)),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.meta?.next_before,
    refetchInterval: (q) =>
      q.state.data?.pages.some((p) => p.data.some((b) => ACTIVE_JOB.has(b.status) || b.verification_status === "running")) ? 2500 : false,
  })
}

export function useBackup(id: string) {
  return useQuery({
    queryKey: keys.backup(id),
    queryFn: () => api.get<BackupDetail>(`/backups/${id}`),
    enabled: !!id,
    refetchInterval: (q) => {
      const d = q.state.data
      if (!d) return false
      const busy =
        ACTIVE_JOB.has(d.backup.status) ||
        d.backup.verification_status === "running" ||
        (d.verification_job && ACTIVE_JOB.has(d.verification_job.status))
      return busy ? 1500 : false
    },
  })
}

export function useCreateBackup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { database_id: string; storage_destination_id?: string; compression?: string; encrypted?: boolean }) =>
      api.post<QueuedJob>("/backups", input),
    onSuccess: () => invalidateBackupViews(qc),
  })
}

export function useVerifyBackup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<QueuedJob>(`/backups/${id}/verify`),
    onSuccess: (_, id) => {
      void qc.invalidateQueries({ queryKey: keys.backup(id) })
      void qc.invalidateQueries({ queryKey: ["backups"] })
    },
  })
}

export function useDeleteBackup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/backups/${id}`),
    onSuccess: () => invalidateBackupViews(qc),
  })
}

// ------------------------------------------------------------------- jobs

export function useJob(id: string | null | undefined) {
  return useQuery({
    queryKey: keys.job(id ?? ""),
    queryFn: () => api.get<{ job: Job; logs: LogEntry[] }>(`/jobs/${id}`),
    enabled: !!id,
    refetchInterval: (q) => (q.state.data && ACTIVE_JOB.has(q.state.data.job.status) ? 1500 : false),
  })
}

export function useCancelJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<Job>(`/jobs/${id}/cancel`),
    onSuccess: () => {
      invalidateBackupViews(qc)
      void qc.invalidateQueries({ queryKey: ["job"] })
      void qc.invalidateQueries({ queryKey: ["restores"] })
      void qc.invalidateQueries({ queryKey: ["restore"] })
    },
  })
}

// --------------------------------------------------------------- restores

export interface RestoreFilters {
  target_database_id?: string
  status?: string
  mode?: string
}

export function useRestores(filters: RestoreFilters = {}) {
  return useInfiniteQuery({
    queryKey: keys.restores(filters),
    queryFn: ({ pageParam }) => api.list<RestoreJob>(`/restores?${searchParams({ ...filters }, pageParam)}`),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.meta?.next_before,
    refetchInterval: (q) => (q.state.data?.pages.some((p) => p.data.some((r) => ACTIVE_RESTORE.has(r.status))) ? 2000 : false),
  })
}

export function useRestore(id: string | null | undefined) {
  return useQuery({
    queryKey: keys.restore(id ?? ""),
    queryFn: () => api.get<{ restore: RestoreJob; job?: Job; logs: LogEntry[] }>(`/restores/${id}`),
    enabled: !!id,
    refetchInterval: (q) => (q.state.data && ACTIVE_RESTORE.has(q.state.data.restore.status) ? 1500 : false),
  })
}

export function useCreateRestore() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: RestoreInput) => api.post<RestoreJob>("/restores", input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["restores"] }),
  })
}

// ---------------------------------------------------------- notifications

export function useNotifications() {
  return useQuery({ queryKey: keys.notifications, queryFn: () => api.get<NotificationChannel[]>("/notifications") })
}

export function useNotificationEvents() {
  return useQuery({
    queryKey: keys.notificationEvents,
    queryFn: () => api.get<{ events: NotificationEventInfo[]; email_configured: boolean }>("/notifications/events"),
    staleTime: 300_000,
  })
}

export interface DeliveryFilters {
  notification_id?: string
  event?: string
  status?: string
}

export function useDeliveries(filters: DeliveryFilters = {}) {
  return useInfiniteQuery({
    queryKey: keys.deliveries(filters),
    queryFn: ({ pageParam }) => api.list<NotificationDelivery>(`/notifications/deliveries?${searchParams({ ...filters }, pageParam)}`),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.meta?.next_before,
    refetchInterval: (q) => (q.state.data?.pages.some((p) => p.data.some((d) => d.status === "pending")) ? 3000 : false),
  })
}

export function useCreateNotification() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: NotificationInput) =>
      api.post<{ notification: NotificationChannel; signing_secret?: string }>("/notifications", input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.notifications }),
  })
}

export function useUpdateNotification(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: NotificationInput) => api.patch<NotificationChannel>(`/notifications/${id}`, input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.notifications }),
  })
}

export function useDeleteNotification() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/notifications/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.notifications }),
  })
}

export function useTestNotification() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<{ delivery_id: string }>(`/notifications/${id}/test`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["notifications", "deliveries"] }),
  })
}

// ------------------------------------------------------------------ audit

export interface AuditFilters {
  action?: string
  resource_type?: string
  resource_id?: string
  limit?: number
}

export function useAuditLogs(filters: AuditFilters = {}) {
  return useInfiniteQuery({
    queryKey: keys.audit(filters),
    queryFn: ({ pageParam }) => {
      const p = new URLSearchParams()
      for (const [k, v] of Object.entries(filters)) if (v !== undefined && v !== "") p.set(k, String(v))
      if (pageParam) p.set("before", pageParam)
      return api.list<AuditLog>(`/audit-logs?${p}`)
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.meta?.next_before,
  })
}

// ------------------------------------------------------- organization/team

export function useOrganization() {
  return useQuery({
    queryKey: keys.organization,
    queryFn: () => api.get<{ id: string; name: string; slug: string; role: Role; member_count: number }>("/organization"),
  })
}

export function useUpdateOrganization() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => api.patch("/organization", { name }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.organization })
      void qc.invalidateQueries({ queryKey: keys.me })
    },
  })
}

export function useCreateOrganization() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => api.post<Organization>("/organizations", { name }, { noOrg: true }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.me }),
  })
}

export function useMembers() {
  return useQuery({ queryKey: keys.members, queryFn: () => api.get<Member[]>("/team/members") })
}

export function useInvitations(enabled = true) {
  return useQuery({ queryKey: keys.invitations, queryFn: () => api.get<Invitation[]>("/team/invitations"), enabled })
}

export function useInvite() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { email: string; role: Exclude<Role, "owner"> }) =>
      api.post<{ invitation: Invitation; email_sent: boolean }>("/team/invitations", input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.invitations }),
  })
}

export function useRevokeInvitation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/team/invitations/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.invitations }),
  })
}

export function useChangeRole() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ userId, role }: { userId: string; role: Role }) => api.patch(`/team/members/${userId}`, { role }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.members }),
  })
}

export function useRemoveMember() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (userId: string) => api.delete(`/team/members/${userId}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.members }),
  })
}

export function useAcceptInvitation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (token: string) => api.post<Organization>("/invitations/accept", { token }, { noOrg: true }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.me }),
  })
}

export function useExportRecoveryKey() {
  return useMutation({
    mutationFn: (password: string) =>
      api.post<{ key_id: string; public_key: string; identity: string; algorithm: string }>("/organization/recovery-key", { password }),
  })
}

// ---------------------------------------------------------------- account

export function useSessions() {
  return useQuery({ queryKey: keys.sessions, queryFn: () => api.get<Session[]>("/auth/sessions", { noOrg: true }) })
}

export function useRevokeSession() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/auth/sessions/${id}`, { noOrg: true }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.sessions }),
  })
}

export function useApiTokens() {
  return useQuery({ queryKey: keys.tokens, queryFn: () => api.get<ApiToken[]>("/auth/tokens", { noOrg: true }) })
}

export function useCreateApiToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { name: string; expires_in_days: number }) =>
      api.post<{ token: string; api_token: ApiToken }>("/auth/tokens", input, { noOrg: true }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.tokens }),
  })
}

export function useRevokeApiToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/auth/tokens/${id}`, { noOrg: true }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.tokens }),
  })
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (input: { current_password: string; new_password: string }) => api.post("/auth/password/change", input, { noOrg: true }),
  })
}

export function useUpdateProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => api.patch("/auth/me", { name }, { noOrg: true }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.me }),
  })
}
