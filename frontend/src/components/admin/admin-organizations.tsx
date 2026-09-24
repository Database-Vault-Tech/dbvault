"use client"

import { AlertOctagon, Archive, Building2, Database, HardDrive, Users } from "lucide-react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { useMemo, useState } from "react"

import { matches, TablePagination, TableSearch, TableToolbar, usePaging } from "@/components/app/data-table"
import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { RelativeTime } from "@/components/app/relative-time"
import { StatCard } from "@/components/app/stat-card"
import { StatusDot } from "@/components/app/status"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatBytes, formatNumber } from "@/lib/format"
import { useAdminOrganizations, useAdminOverview } from "@/lib/queries"

import { AdminShell } from "./admin-shell"

export function AdminOrganizations() {
  const router = useRouter()
  const overview = useAdminOverview()
  const { data, isPending, error, refetch } = useAdminOrganizations()
  const [search, setSearch] = useState("")
  const rows = useMemo(() => (data ?? []).filter((o) => matches(search, o.name, o.slug, o.owner_email)), [data, search])
  const paging = usePaging(rows, search)
  const o = overview.data
  const loading = overview.isPending

  return (
    <AdminShell>
      <div className="mb-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
        <StatCard label="Organizations" icon={Building2} loading={loading} value={formatNumber(o?.organizations ?? 0)} />
        <StatCard label="Users" icon={Users} loading={loading} value={formatNumber(o?.users ?? 0)} />
        <StatCard label="Databases" icon={Database} loading={loading} value={formatNumber(o?.databases ?? 0)} />
        <StatCard label="Backups" icon={Archive} loading={loading} value={formatNumber(o?.backups ?? 0)} hint={o?.active_jobs ? `${o.active_jobs} jobs queued or running` : "Completed and kept"} />
        <StatCard label="Storage" icon={HardDrive} loading={loading} value={formatBytes(o?.storage_bytes ?? 0)} hint="All organizations" />
        <StatCard
          label="Failed"
          icon={AlertOctagon}
          loading={loading}
          tone={o && o.failed_backups_7d > 0 ? "error" : "default"}
          value={formatNumber(o?.failed_backups_7d ?? 0)}
          hint="Backups, last 7 days"
        />
      </div>

      {!isPending && !error && (data?.length ?? 0) > 0 && (
        <TableToolbar>
          <TableSearch value={search} onChange={setSearch} placeholder="Search name, slug, owner…" />
        </TableToolbar>
      )}
      {error ? (
        <ErrorState error={error} retry={() => refetch()} />
      ) : isPending ? (
        <TableSkeleton rows={5} columns={6} />
      ) : rows.length === 0 ? (
        <EmptyState icon={Building2} title={search ? "No organizations match" : "No organizations yet"} description={search ? "Try a different search." : "They appear here as people sign up."} />
      ) : (
        <div className="space-y-3">
          <div className="overflow-hidden rounded-xl border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="pl-4">Organization</TableHead>
                  <TableHead className="hidden text-right sm:table-cell">Members</TableHead>
                  <TableHead className="text-right">Databases</TableHead>
                  <TableHead className="hidden text-right md:table-cell">Storage</TableHead>
                  <TableHead className="hidden lg:table-cell">Last backup</TableHead>
                  <TableHead className="hidden pr-4 xl:table-cell">Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {paging.rows.map((org) => (
                  <TableRow key={org.id} className="cursor-pointer" onClick={() => router.push(`/admin/organizations/${org.id}`)}>
                    <TableCell className="pl-4">
                      <Link href={`/admin/organizations/${org.id}`} className="font-medium hover:underline" onClick={(e) => e.stopPropagation()}>
                        {org.name}
                      </Link>
                      <div className="max-w-72 truncate text-xs text-muted-foreground">{org.owner_email ?? "No owner"}</div>
                    </TableCell>
                    <TableCell className="hidden text-right tabular sm:table-cell">{org.members}</TableCell>
                    <TableCell className="text-right tabular">{org.databases}</TableCell>
                    <TableCell className="hidden text-right tabular md:table-cell">{formatBytes(org.storage_bytes)}</TableCell>
                    <TableCell className="hidden lg:table-cell">
                      {org.last_backup_at ? (
                        <span className="inline-flex items-center gap-2 text-sm text-muted-foreground">
                          <StatusDot tone={org.failed_backups_7d > 0 ? "error" : "success"} />
                          <RelativeTime date={org.last_backup_at} />
                          {org.failed_backups_7d > 0 && <span className="text-destructive">· {org.failed_backups_7d} failed</span>}
                        </span>
                      ) : (
                        <span className="text-sm text-muted-foreground">Never</span>
                      )}
                    </TableCell>
                    <TableCell className="hidden pr-4 text-muted-foreground xl:table-cell">
                      <RelativeTime date={org.created_at} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <TablePagination {...paging.props} noun="organizations" />
        </div>
      )}
    </AdminShell>
  )
}
