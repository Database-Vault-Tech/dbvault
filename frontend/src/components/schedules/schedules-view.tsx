"use client"

import { Archive, CalendarClock, FlaskConical, Lock, LockOpen, MoreHorizontal, Pencil, Play, Plus, Trash2 } from "lucide-react"
import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { useState } from "react"
import { toast } from "sonner"

import { ConfirmDialog } from "@/components/app/confirm-dialog"
import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { PageHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { StatusBadge } from "@/components/app/status"
import { StorageBadge } from "@/components/app/storage-badge"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { errorMessage } from "@/lib/api"
import { useOrg } from "@/lib/org"
import { useDatabases, useDeleteSchedule, useRunSchedule, useSchedules, useStorage, useUpdateSchedule } from "@/lib/queries"
import type { Schedule } from "@/lib/types"

import { RetentionDialog } from "./retention-dialog"
import { ScheduleDialog } from "./schedule-form"
import { retentionSummary, scheduleToInput } from "./utils"

export function SchedulesView() {
  const params = useSearchParams()
  const router = useRouter()
  const { can } = useOrg()
  const schedules = useSchedules()
  const databases = useDatabases()
  const storage = useStorage()
  const [creating, setCreating] = useState(params.get("new") === "1")
  const [editing, setEditing] = useState<Schedule>()
  const [retention, setRetention] = useState<Schedule>()
  const [deleting, setDeleting] = useState<Schedule>()
  const del = useDeleteSchedule()

  const closeCreate = (o: boolean) => {
    setCreating(o)
    if (!o && (params.get("new") || params.get("database"))) router.replace("/schedules")
  }
  const noDatabases = databases.data?.length === 0
  const noStorage = storage.data?.length === 0

  return (
    <div className="space-y-6">
      <PageHeader
        title="Schedules"
        description="Automated backups with retention. The scheduler runs on the server, never in your browser."
        actions={
          can("admin") && (
            <Button onClick={() => setCreating(true)} disabled={noDatabases || noStorage}>
              <Plus /> Create schedule
            </Button>
          )
        }
      />
      {schedules.error && <ErrorState error={schedules.error} retry={() => schedules.refetch()} />}
      {schedules.isPending ? (
        <TableSkeleton rows={4} columns={6} />
      ) : noDatabases && schedules.data?.length === 0 ? (
        <EmptyState
          icon={CalendarClock}
          title="Add a database first"
          description="Schedules back up a connected database."
          action={
            <Button asChild>
              <Link href="/databases/new">Add database</Link>
            </Button>
          }
        />
      ) : noStorage && schedules.data?.length === 0 ? (
        <EmptyState
          icon={CalendarClock}
          title="Add a storage destination first"
          description="Schedules need somewhere to upload backups."
          action={
            <Button asChild>
              <Link href="/storage?new=1">Add storage</Link>
            </Button>
          }
        />
      ) : schedules.data?.length === 0 ? (
        <EmptyState
          icon={CalendarClock}
          title="No schedules yet"
          description="Create a schedule and DBVault will back up, compress, encrypt and prune automatically."
          action={
            can("admin") && (
              <Button onClick={() => setCreating(true)}>
                <Plus /> Create schedule
              </Button>
            )
          }
        />
      ) : (
        <div className="overflow-hidden rounded-xl border bg-card">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-4">Database</TableHead>
                <TableHead>Frequency</TableHead>
                <TableHead>Next run</TableHead>
                <TableHead className="hidden md:table-cell">Last run</TableHead>
                <TableHead className="hidden lg:table-cell">Storage</TableHead>
                <TableHead className="hidden lg:table-cell">Retention</TableHead>
                <TableHead className="hidden xl:table-cell">Options</TableHead>
                <TableHead>Enabled</TableHead>
                <TableHead className="w-10 pr-4" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {schedules.data?.map((s) => (
                <ScheduleRow
                  key={s.id}
                  s={s}
                  onEdit={() => setEditing(s)}
                  onRetention={() => setRetention(s)}
                  onDelete={() => setDeleting(s)}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <ScheduleDialog open={creating} onOpenChange={closeCreate} initialDatabaseId={params.get("database") ?? undefined} />
      <ScheduleDialog open={!!editing} onOpenChange={(o) => !o && setEditing(undefined)} schedule={editing} />
      <RetentionDialog schedule={retention} onOpenChange={(o) => !o && setRetention(undefined)} />
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(undefined)}
        title="Delete schedule?"
        description={<p>Automatic backups of {deleting?.database_name} stop. Existing backups are kept and remain restorable.</p>}
        confirmLabel="Delete schedule"
        pending={del.isPending}
        onConfirm={() =>
          deleting &&
          del.mutate(deleting.id, {
            onSuccess: () => {
              toast.success("Schedule deleted")
              setDeleting(undefined)
            },
            onError: (e) => toast.error(errorMessage(e)),
          })
        }
      />
    </div>
  )
}

function ScheduleRow({ s, onEdit, onRetention, onDelete }: { s: Schedule; onEdit: () => void; onRetention: () => void; onDelete: () => void }) {
  const { can } = useOrg()
  const router = useRouter()
  const update = useUpdateSchedule(s.id)
  const run = useRunSchedule()
  const runNow = () =>
    run.mutate(s.id, {
      onSuccess: (q) =>
        toast.success(`Backup of ${s.database_name} queued`, {
          action: { label: "View", onClick: () => router.push(`/backups/${q.backup_id}`) },
        }),
      onError: (e) => toast.error("Couldn't start backup", { description: errorMessage(e) }),
    })
  return (
    <TableRow>
      <TableCell className="pl-4 font-medium">
        <Link href={`/databases/${s.database_id}`} className="hover:underline">
          {s.database_name}
        </Link>
      </TableCell>
      <TableCell>
        <div>{s.description}</div>
        <div className="font-mono text-xs text-muted-foreground">
          {s.cron_expression} · {s.timezone}
        </div>
      </TableCell>
      <TableCell className="text-muted-foreground">{s.enabled ? <RelativeTime date={s.next_run_at} /> : "Paused"}</TableCell>
      <TableCell className="hidden md:table-cell">
        {s.last_run_at ? (
          <div className="flex flex-col items-start gap-1">
            {s.last_backup_status && <StatusBadge status={s.last_backup_status} />}
            <RelativeTime date={s.last_run_at} className="text-xs text-muted-foreground" />
          </div>
        ) : (
          <span className="text-muted-foreground">Never</span>
        )}
      </TableCell>
      <TableCell className="hidden lg:table-cell">
        <StorageBadge type={s.storage_type} name={s.storage_name} />
      </TableCell>
      <TableCell className="hidden text-sm lg:table-cell">{retentionSummary(s.retention)}</TableCell>
      <TableCell className="hidden xl:table-cell">
        <div className="flex items-center gap-1.5 text-muted-foreground">
          <Badge variant="outline" className="font-mono">
            {s.compression}
          </Badge>
          <Tooltip>
            <TooltipTrigger>{s.encryption ? <Lock className="size-3.5 text-success" /> : <LockOpen className="size-3.5 text-warning" />}</TooltipTrigger>
            <TooltipContent>{s.encryption ? "Encrypted" : "Not encrypted"}</TooltipContent>
          </Tooltip>
          {s.verify_after_backup && (
            <Tooltip>
              <TooltipTrigger>
                <FlaskConical className="size-3.5 text-brand" />
              </TooltipTrigger>
              <TooltipContent>Restore-verified after each backup</TooltipContent>
            </Tooltip>
          )}
        </div>
      </TableCell>
      <TableCell>
        <Switch
          checked={s.enabled}
          disabled={!can("admin") || update.isPending}
          aria-label={s.enabled ? "Disable schedule" : "Enable schedule"}
          onCheckedChange={(enabled) =>
            update.mutate(scheduleToInput(s, { enabled }), {
              onSuccess: () => toast.success(enabled ? "Schedule enabled" : "Schedule paused"),
              onError: (e) => toast.error(errorMessage(e)),
            })
          }
        />
      </TableCell>
      <TableCell className="pr-4">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon-sm" aria-label="Schedule actions">
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {can("member") && (
              <DropdownMenuItem onSelect={runNow} disabled={run.isPending}>
                <Play /> Run now
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onSelect={onRetention}>
              <Archive /> Retention preview
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link href={`/backups?schedule_id=${s.id}`}>
                <CalendarClock /> Backups ({s.backup_count})
              </Link>
            </DropdownMenuItem>
            {can("admin") && (
              <>
                <DropdownMenuItem onSelect={onEdit}>
                  <Pencil /> Edit
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onSelect={onDelete}>
                  <Trash2 /> Delete
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </TableCell>
    </TableRow>
  )
}
