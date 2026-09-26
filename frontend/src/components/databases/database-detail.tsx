"use client"

import { Activity, ArrowLeft, CalendarClock, CheckCircle2, EyeOff, Pencil, PlugZap, Plus, ShieldCheck, Trash2, XCircle } from "lucide-react"
import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { useState } from "react"
import { toast } from "sonner"

import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { PageHeader, SectionHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { StatCard } from "@/components/app/stat-card"
import { StatusBadge } from "@/components/app/status"
import { StorageBadge } from "@/components/app/storage-badge"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { BackupsTable } from "@/components/dashboard/recent-backups"
import { RunBackupButton } from "@/components/dashboard/run-backup-button"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { databaseLocation, engineMeta } from "@/lib/engines"
import { formatBytes, formatPercent } from "@/lib/format"
import { useOrg } from "@/lib/org"
import { unruledPersonal } from "@/lib/masking"
import { useBackups, useDatabase, useMaskingEditor, useSchedules, useUpdateDatabase } from "@/lib/queries"
import type { Database } from "@/lib/types"

import { DeleteDatabaseDialog, useTestDatabaseToast } from "./database-actions"
import { DatabaseForm } from "./database-form"

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 py-2 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 truncate text-right">{children}</dd>
    </div>
  )
}

function EditDialog({ db, open, onOpenChange }: { db: Database; open: boolean; onOpenChange: (o: boolean) => void }) {
  const update = useUpdateDatabase(db.id)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Edit {db.name}</DialogTitle>
          <DialogDescription>Changing connection settings resets the last connection test.</DialogDescription>
        </DialogHeader>
        <DatabaseForm
          mode="edit"
          databaseId={db.id}
          submitLabel="Save changes"
          onCancel={() => onOpenChange(false)}
          defaultValues={{ engine: engineMeta(db.engine).id, name: db.name, host: db.host, port: db.port, database: db.database, username: db.username, ssl_mode: db.ssl_mode, password: "" }}
          onSubmit={async (input) => {
            await update.mutateAsync(input)
            toast.success("Database updated")
            onOpenChange(false)
          }}
        />
      </DialogContent>
    </Dialog>
  )
}

function BackupHistory({ databaseId }: { databaseId: string }) {
  const q = useBackups({ database_id: databaseId })
  const backups = q.data?.pages.flatMap((p) => p.data) ?? []
  return (
    <section>
      <SectionHeader title="Backup history" description="Every backup of this database, newest first." />
      {q.error ? (
        <ErrorState error={q.error} retry={() => q.refetch()} />
      ) : q.isPending ? (
        <TableSkeleton rows={4} columns={5} />
      ) : backups.length === 0 ? (
        <EmptyState icon={Activity} title="No backups yet" description="Run a backup now or create a schedule to start building history." action={<RunBackupButton databaseId={databaseId} />} />
      ) : (
        <div className="space-y-3">
          <BackupsTable backups={backups} showDatabase={false} />
          {q.hasNextPage && (
            <div className="flex justify-center">
              <Button variant="outline" size="sm" onClick={() => q.fetchNextPage()} disabled={q.isFetchingNextPage}>
                {q.isFetchingNextPage && <Spinner />} Load more
              </Button>
            </div>
          )}
        </div>
      )}
    </section>
  )
}

function SchedulesCard({ databaseId }: { databaseId: string }) {
  const { can } = useOrg()
  const { data, isPending } = useSchedules(databaseId)
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="flex items-center gap-2 text-sm">
          <CalendarClock className="size-4 text-muted-foreground" /> Schedules
        </CardTitle>
        <div className="flex gap-1">
          {!!data?.length && (
            <Button variant="ghost" size="sm" asChild>
              <Link href={`/schedules?database=${databaseId}`}>Manage</Link>
            </Button>
          )}
          {can("admin") && (
            <Button variant="outline" size="sm" asChild>
              <Link href={`/schedules?new=1&database=${databaseId}`}>
                <Plus /> Create schedule
              </Link>
            </Button>
          )}
        </div>
      </CardHeader>
      <CardContent>
        {isPending ? (
          <Skeleton className="h-16" />
        ) : !data?.length ? (
          <p className="text-sm text-muted-foreground">No schedule. This database is only backed up when you run a backup manually.</p>
        ) : (
          <ul className="divide-y">
            {data.map((s) => (
              <li key={s.id} className="flex flex-wrap items-center justify-between gap-2 py-2.5 text-sm">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 font-medium">
                    {s.description}
                    {!s.enabled && <StatusBadge status="cancelled" label="Paused" />}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    Retention: {s.retention.daily} daily · {s.retention.weekly} weekly · {s.retention.monthly} monthly
                  </div>
                </div>
                <div className="flex items-center gap-4 text-xs text-muted-foreground">
                  <StorageBadge type={s.storage_type} name={s.storage_name} className="text-xs" />
                  {s.enabled && s.next_run_at && (
                    <span>
                      Next <RelativeTime date={s.next_run_at} />
                    </span>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

export function DatabaseDetail({ id }: { id: string }) {
  const { can } = useOrg()
  const router = useRouter()
  const params = useSearchParams()
  const { data, isPending, error, refetch } = useDatabase(id)
  const [editing, setEditing] = useState(params.get("edit") === "1")
  const [deleting, setDeleting] = useState(false)
  const test = useTestDatabaseToast()

  if (error) return <ErrorState error={error} retry={() => refetch()} title="Couldn't load this database" />
  if (isPending) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-10 w-64" />
        <div className="grid gap-4 md:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-28" />
          ))}
        </div>
        <TableSkeleton rows={4} />
      </div>
    )
  }
  const { database: db, health } = data

  return (
    <div className="space-y-8">
      <div>
        <Button variant="ghost" size="sm" asChild className="mb-4 -ml-2 text-muted-foreground">
          <Link href="/databases">
            <ArrowLeft /> Databases
          </Link>
        </Button>
        <PageHeader
          className="pb-0"
          title={
            <span className="flex items-center gap-3">
              {db.name}
              <StatusBadge status={health.status} />
            </span>
          }
          description={
            <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="font-mono text-xs">{databaseLocation(db)}</span>
              <span>
                {engineMeta(db.engine).label}
                {db.pg_version && ` ${db.pg_version}`}
              </span>
              {db.size_bytes !== null && <span>{formatBytes(db.size_bytes)}</span>}
              <span className="text-muted-foreground">· {health.reason}</span>
            </span>
          }
          actions={
            <>
              {can("member") && (
                <Button variant="outline" onClick={() => test.run(db)} disabled={test.pending}>
                  {test.pending ? <Spinner /> : <PlugZap />} Test connection
                </Button>
              )}
              {can("admin") && (
                <>
                  <Button variant="outline" onClick={() => setEditing(true)}>
                    <Pencil /> Edit
                  </Button>
                  <Button variant="outline" size="icon" aria-label="Delete database" onClick={() => setDeleting(true)}>
                    <Trash2 className="text-destructive" />
                  </Button>
                </>
              )}
              <RunBackupButton databaseId={db.id} />
            </>
          }
        />
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label="Success rate (30d)"
          icon={ShieldCheck}
          value={formatPercent(health.success_rate_30d)}
          tone={health.success_rate_30d !== null && health.success_rate_30d < 1 ? "error" : "default"}
          hint={`${health.completed_30d} completed · ${health.failed_30d} failed`}
        />
        <StatCard label="Backups stored" icon={Activity} value={db.backup_count} hint={`${formatBytes(db.storage_bytes)} in storage`} />
        <StatCard
          label="Last success"
          icon={CheckCircle2}
          value={db.last_success_at ? <RelativeTime date={db.last_success_at} className="text-xl" /> : <span className="text-xl text-muted-foreground">Never</span>}
          hint={health.last_failure_at ? <>Last failure <RelativeTime date={health.last_failure_at} /></> : "No failures in 30 days"}
        />
        <StatCard
          label="Last verified"
          icon={health.last_verified_at ? CheckCircle2 : XCircle}
          tone={health.last_verified_at ? "success" : "default"}
          value={health.last_verified_at ? <RelativeTime date={health.last_verified_at} className="text-xl" /> : <span className="text-xl text-muted-foreground">Never</span>}
          hint="Restore-tested in a sandbox"
        />
      </div>

      <div className="grid gap-6 lg:grid-cols-[360px_1fr]">
        <div className="space-y-6">
          <Card className="h-fit">
            <CardHeader>
              <CardTitle className="text-sm">Connection</CardTitle>
            </CardHeader>
            <CardContent>
              <dl className="divide-y">
                {engineMeta(db.engine).fileBased ? (
                  <>
                    <Row label="File">
                      <span className="font-mono text-xs break-all">{db.database}</span>
                    </Row>
                    <Row label="Location">
                      <span className="text-muted-foreground">SQLite folder (SQLITE_ROOT)</span>
                    </Row>
                  </>
                ) : (
                  <>
                    <Row label="Host">
                      <span className="font-mono text-xs">{db.host}</span>
                    </Row>
                    <Row label="Port">
                      <span className="font-mono text-xs">{db.port}</span>
                    </Row>
                    <Row label="Database">
                      <span className="font-mono text-xs">{db.database}</span>
                    </Row>
                    <Row label="Username">
                      <span className="font-mono text-xs">{db.username}</span>
                    </Row>
                    <Row label="Password">
                      <span className="text-muted-foreground">•••••••• (encrypted)</span>
                    </Row>
                    <Row label="SSL mode">
                      <span className="font-mono text-xs">{db.ssl_mode}</span>
                    </Row>
                    <Row label="CA certificate">{db.has_ssl_root_cert ? "Provided" : "None"}</Row>
                  </>
                )}
                <Row label="Last tested">
                  {db.last_tested_at ? (
                    <span className="inline-flex items-center gap-2">
                      <StatusBadge status={db.last_test_ok ? "ok" : "failed"} label={db.last_test_ok ? "OK" : "Failed"} />
                      <RelativeTime date={db.last_tested_at} className="text-muted-foreground" />
                    </span>
                  ) : (
                    "Never"
                  )}
                </Row>
              </dl>
              {db.last_test_ok === false && db.last_test_error && <p className="mt-2 text-xs text-destructive">{db.last_test_error}</p>}
            </CardContent>
          </Card>
          <MaskingCard databaseId={db.id} />
        </div>
        <SchedulesCard databaseId={db.id} />
      </div>

      <BackupHistory databaseId={db.id} />

      {can("admin") && (
        <>
          <EditDialog db={db} open={editing} onOpenChange={setEditing} />
          <DeleteDatabaseDialog db={db} open={deleting} onOpenChange={setDeleting} onDeleted={() => router.push("/databases")} />
        </>
      )}
    </div>
  )
}

/** Masking status for the database, linking to the profile editor. */
function MaskingCard({ databaseId }: { databaseId: string }) {
  const { data } = useMaskingEditor(databaseId)
  if (!data?.supported) return null
  const profile = data.profiles.find((p) => p.name === "default")
  const undecided = profile && data.schema ? unruledPersonal(profile.rules, data.schema.tables).length : 0
  const problems = profile ? (data.problems.default ?? []).length : 0
  const columns = profile ? Object.values(profile.rules.tables).reduce((n, t) => n + (t === "truncate" ? 0 : Object.keys(t).length), 0) : 0
  return (
    <Card className="h-fit">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-sm">
          <EyeOff className="size-4 text-muted-foreground" /> Data masking
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        {profile ? (
          <p className="text-muted-foreground">
            Profile <span className="font-mono text-foreground">default</span> v{profile.version}: {columns} columns masked,{" "}
            {Object.values(profile.rules.tables).filter((t) => t === "truncate").length} tables emptied.
            {problems + undecided > 0 && <span className="block text-warning">Needs attention: a masked restore would stop.</span>}
          </p>
        ) : (
          <p className="text-muted-foreground">Restore anonymized copies into staging: personal data is replaced with realistic fakes before it leaves a sandbox.</p>
        )}
        <Button asChild variant="outline" size="sm">
          <Link href={`/databases/${databaseId}/masking`}>{profile ? "Edit masking rules" : "Set up masking"}</Link>
        </Button>
      </CardContent>
    </Card>
  )
}
