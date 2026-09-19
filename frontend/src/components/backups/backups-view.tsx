"use client"

import { Archive } from "lucide-react"
import { useMemo, useState } from "react"

import { ALL, ClearFiltersButton, FilterSelect, matches, TablePagination, TableSearch, TableToolbar, useSearchPaging } from "@/components/app/data-table"
import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { PageHeader } from "@/components/app/page-header"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { BackupsTable } from "@/components/dashboard/recent-backups"
import { RunBackupButton } from "@/components/dashboard/run-backup-button"
import { useBackups, useDatabases, type BackupFilters } from "@/lib/queries"

const STATUSES = ["completed", "running", "queued", "failed", "cancelled", "deleted"]

export function BackupsView() {
  const [databaseId, setDatabaseId] = useState(ALL)
  const [status, setStatus] = useState(ALL)
  const [trigger, setTrigger] = useState(ALL)
  const [search, setSearch] = useState("")
  const { data: databases } = useDatabases()
  const filters: BackupFilters = {
    database_id: databaseId === ALL ? undefined : databaseId,
    status: status === ALL ? undefined : status,
    trigger: trigger === ALL ? undefined : trigger,
  }
  const q = useBackups(filters)
  // The search narrows what's already loaded (database name, storage, key).
  const searched = useMemo(() => {
    const rows = q.data?.pages.flatMap((p) => p.data) ?? []
    return rows.filter((b) => matches(search, b.database_name, b.storage_name, b.storage_key, b.status))
  }, [q.data, search])
  const paging = useSearchPaging(searched, q, JSON.stringify({ filters, search }))
  const filtered = databaseId !== ALL || status !== ALL || trigger !== ALL || search.trim() !== ""

  return (
    <div>
      <PageHeader
        title="Backups"
        description="Every backup job: status, size, checksum and where it's stored. Click a backup for logs, verification and downloads."
        actions={<RunBackupButton />}
      />
      <TableToolbar>
        <TableSearch value={search} onChange={setSearch} placeholder="Search database, storage…" />
        <FilterSelect
          value={databaseId}
          onChange={setDatabaseId}
          label="Filter by database"
          allLabel="All databases"
          className="w-48"
          options={(databases ?? []).map((d) => ({ value: d.id, label: d.name }))}
        />
        <FilterSelect
          value={status}
          onChange={setStatus}
          label="Filter by status"
          allLabel="All statuses"
          options={STATUSES.map((s) => ({ value: s, label: s.charAt(0).toUpperCase() + s.slice(1) }))}
        />
        <FilterSelect
          value={trigger}
          onChange={setTrigger}
          label="Filter by trigger"
          allLabel="Any trigger"
          options={[
            { value: "manual", label: "Manual" },
            { value: "scheduled", label: "Scheduled" },
          ]}
        />
        <ClearFiltersButton
          show={filtered}
          onClear={() => {
            setDatabaseId(ALL)
            setStatus(ALL)
            setTrigger(ALL)
            setSearch("")
          }}
        />
      </TableToolbar>
      {q.error ? (
        <ErrorState error={q.error} retry={() => q.refetch()} />
      ) : q.isPending ? (
        <TableSkeleton rows={6} columns={6} />
      ) : paging.props.total === 0 ? (
        <EmptyState
          icon={Archive}
          title={filtered ? "No backups match these filters" : "No backups yet"}
          description={
            filtered ? "Try a different database, status, trigger or search." : "Run a backup now, or create a schedule and DBVault will take it from there."
          }
          action={!filtered && <RunBackupButton />}
        />
      ) : (
        <div className="space-y-3">
          <BackupsTable backups={paging.rows} />
          <TablePagination {...paging.props} noun="backups" />
        </div>
      )}
    </div>
  )
}
