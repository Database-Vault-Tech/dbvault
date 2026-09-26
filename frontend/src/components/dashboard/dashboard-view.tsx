"use client"

import { AlertOctagon, Archive, CheckCircle2, Database, HardDrive, ShieldCheck } from "lucide-react"
import Link from "next/link"

import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { PageHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { StatCard } from "@/components/app/stat-card"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { formatBytes } from "@/lib/format"
import { useOrg } from "@/lib/org"
import { useDashboard, useSchedules, useStorage } from "@/lib/queries"

import { ActivityChart } from "./activity-chart"
import { Onboarding } from "./onboarding"
import { RecentBackups } from "./recent-backups"
import { RunBackupButton } from "./run-backup-button"

export function DashboardView() {
  const { me, can } = useOrg()
  const dashboard = useDashboard()
  const storage = useStorage()
  const schedules = useSchedules()
  const s = dashboard.data?.stats
  const loading = dashboard.isPending
  const firstName = me.user.name.split(" ")[0]

  return (
    <div className="space-y-8">
      <PageHeader
        eyebrow={`Welcome back, ${firstName}`}
        title="Overview"
        description="Backup health across every database in this organization."
        actions={
          <>
            {can("admin") && (
              <Button variant="outline" asChild>
                <Link href="/databases/new">
                  <Database /> Add database
                </Link>
              </Button>
            )}
            <RunBackupButton />
          </>
        }
      />

      {dashboard.error && <ErrorState error={dashboard.error} retry={() => dashboard.refetch()} />}

      {s && storage.data && schedules.data && (
        <Onboarding databases={s.databases} storage={storage.data.length} schedules={schedules.data.length} backups={dashboard.data?.recent_backups.length ?? 0} />
      )}

      <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-3 xl:grid-cols-6">
        <StatCard label="Databases" icon={Database} loading={loading} value={s?.databases ?? 0} hint="Connected databases" />
        <StatCard
          label="Protected"
          icon={ShieldCheck}
          loading={loading}
          tone={s && s.databases > 0 && s.protected_databases === s.databases ? "success" : "default"}
          value={s ? `${s.protected_databases}` : 0}
          hint={s && s.databases > s.protected_databases ? `${s.databases - s.protected_databases} without a schedule` : "With an active schedule"}
        />
        <StatCard label="Backups today" icon={Archive} loading={loading} value={s?.backups_today ?? 0} hint={s?.running_jobs ? `${s.running_jobs} running now` : "Completed since midnight"} />
        <StatCard label="Storage" icon={HardDrive} loading={loading} value={formatBytes(s?.storage_used_bytes ?? 0)} hint="Across all destinations" />
        <StatCard
          label="Failed"
          icon={AlertOctagon}
          loading={loading}
          tone={s && s.failed_backups_7d > 0 ? "error" : "default"}
          value={s?.failed_backups_7d ?? 0}
          hint="In the last 7 days"
        />
        <StatCard
          label="Last success"
          icon={CheckCircle2}
          loading={loading}
          value={s?.last_successful_backup ? <RelativeTime date={s.last_successful_backup} className="text-xl" /> : <span className="text-xl text-muted-foreground">Never</span>}
          hint={s?.verified_backups_30d ? `${s.verified_backups_30d} restore-verified in 30 days` : "No verified backups yet"}
        />
      </div>

      <div className="grid gap-6 xl:grid-cols-[1fr_320px]">
        <div className="min-w-0">
          {loading ? (
            <TableSkeleton rows={6} columns={6} />
          ) : dashboard.data && dashboard.data.recent_backups.length > 0 ? (
            <RecentBackups backups={dashboard.data.recent_backups} />
          ) : (
            <EmptyState
              icon={Archive}
              title="No backups yet"
              description={
                s?.databases
                  ? "Run a backup now, or create a schedule and DBVault will take it from there."
                  : "Connect your first PostgreSQL, MySQL, MariaDB or SQLite database and DBVault will start protecting it."
              }
              action={
                s?.databases ? (
                  <RunBackupButton />
                ) : can("admin") ? (
                  <Button asChild>
                    <Link href="/databases/new">
                      <Database /> Add database
                    </Link>
                  </Button>
                ) : undefined
              }
            />
          )}
        </div>
        <Card className="h-fit">
          <CardHeader>
            <CardTitle className="text-sm">Last 14 days</CardTitle>
            <CardDescription>Completed and failed backups per day.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {s ? <ActivityChart days={s.daily} /> : <div className="h-36 animate-pulse rounded-md bg-muted" />}
            <div className="flex gap-4 text-xs text-muted-foreground">
              <span className="flex items-center gap-1.5">
                <span className="size-2 rounded-sm bg-brand/70" /> Completed
              </span>
              <span className="flex items-center gap-1.5">
                <span className="size-2 rounded-sm bg-destructive/80" /> Failed
              </span>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
