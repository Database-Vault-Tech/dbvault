"use client"

import { Archive } from "lucide-react"
import { useState } from "react"

import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { PageHeader } from "@/components/app/page-header"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { BackupsTable } from "@/components/dashboard/recent-backups"
import { RunBackupButton } from "@/components/dashboard/run-backup-button"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { useBackups, useDatabases, type BackupFilters } from "@/lib/queries"

const ALL = "all"
const STATUSES = ["completed", "running", "queued", "failed", "cancelled", "deleted"]

export function BackupsView() {
  const [databaseId, setDatabaseId] = useState(ALL)
  const [status, setStatus] = useState(ALL)
  const [trigger, setTrigger] = useState(ALL)
  const { data: databases } = useDatabases()
  const filters: BackupFilters = {
    database_id: databaseId === ALL ? undefined : databaseId,
    status: status === ALL ? undefined : status,
    trigger: trigger === ALL ? undefined : trigger,
  }
  const q = useBackups(filters)
  const backups = q.data?.pages.flatMap((p) => p.data) ?? []
  const filtered = databaseId !== ALL || status !== ALL || trigger !== ALL

  return (
    <div>
      <PageHeader
        title="Backups"
        description="Every backup job: status, size, checksum and where it's stored. Click a backup for logs, verification and downloads."
        actions={<RunBackupButton />}
      />
      <div className="mb-4 flex flex-wrap gap-2">
        <Select value={databaseId} onValueChange={setDatabaseId}>
          <SelectTrigger className="w-48" aria-label="Filter by database">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All databases</SelectItem>
            {databases?.map((d) => (
              <SelectItem key={d.id} value={d.id}>
                {d.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={status} onValueChange={setStatus}>
          <SelectTrigger className="w-40" aria-label="Filter by status">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All statuses</SelectItem>
            {STATUSES.map((s) => (
              <SelectItem key={s} value={s} className="capitalize">
                {s}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={trigger} onValueChange={setTrigger}>
          <SelectTrigger className="w-40" aria-label="Filter by trigger">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>Any trigger</SelectItem>
            <SelectItem value="manual">Manual</SelectItem>
            <SelectItem value="scheduled">Scheduled</SelectItem>
          </SelectContent>
        </Select>
        {filtered && (
          <Button
            variant="ghost"
            onClick={() => {
              setDatabaseId(ALL)
              setStatus(ALL)
              setTrigger(ALL)
            }}
          >
            Clear filters
          </Button>
        )}
      </div>
      {q.error ? (
        <ErrorState error={q.error} retry={() => q.refetch()} />
      ) : q.isPending ? (
        <TableSkeleton rows={6} columns={6} />
      ) : backups.length === 0 ? (
        <EmptyState
          icon={Archive}
          title={filtered ? "No backups match these filters" : "No backups yet"}
          description={filtered ? "Try a different database, status or trigger." : "Run a backup now, or create a schedule and DBVault will take it from there."}
          action={!filtered && <RunBackupButton />}
        />
      ) : (
        <div className="space-y-3">
          <BackupsTable backups={backups} />
          {q.hasNextPage && (
            <div className="flex justify-center">
              <Button variant="outline" size="sm" onClick={() => q.fetchNextPage()} disabled={q.isFetchingNextPage}>
                {q.isFetchingNextPage && <Spinner />} Load more
              </Button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
