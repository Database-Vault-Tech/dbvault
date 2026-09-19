"use client"

import { ChevronRight, ScrollText } from "lucide-react"
import Link from "next/link"
import { Fragment, useMemo, useState } from "react"

import { ALL, ClearFiltersButton, FilterSelect, matches, TablePagination, TableSearch, TableToolbar, useSearchPaging } from "@/components/app/data-table"
import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { PageHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { humanizeAction, shortId } from "@/lib/format"
import { useAuditLogs } from "@/lib/queries"
import type { AuditLog } from "@/lib/types"
import { cn } from "@/lib/utils"

const CATEGORIES = [
  { value: "user.", label: "Authentication" },
  { value: "database.", label: "Databases" },
  { value: "backup.", label: "Backups" },
  { value: "restore.", label: "Restores" },
  { value: "storage.", label: "Storage" },
  { value: "schedule.", label: "Schedules" },
  { value: "member.", label: "Team" },
  { value: "notification.", label: "Notifications" },
]

function resourceHref(l: AuditLog): string | undefined {
  if (!l.resource_id) return undefined
  if (l.resource_type === "backup") return `/backups/${l.resource_id}`
  if (l.resource_type === "database" && l.action !== "database.deleted") return `/databases/${l.resource_id}`
  return undefined
}

const RESOURCE_TYPES = ["database", "backup", "restore", "schedule", "storage_destination", "notification", "user", "api_token"]

export function AuditLogView() {
  const [category, setCategory] = useState(ALL)
  const [resourceType, setResourceType] = useState(ALL)
  const [search, setSearch] = useState("")
  const [open, setOpen] = useState<string | null>(null)
  const filters = {
    action: category === ALL ? undefined : category,
    resource_type: resourceType === ALL ? undefined : resourceType,
    limit: 50,
  }
  const logs = useAuditLogs(filters)
  const filtered = category !== ALL || resourceType !== ALL || search.trim() !== ""
  // The search runs over the rows already fetched (actor, action, resource).
  const searched = useMemo(() => {
    const all = logs.data?.pages.flatMap((p) => p.data) ?? []
    return all.filter((l) => matches(search, l.actor_email, l.action, humanizeAction(l.action), l.resource_type, l.resource_id, l.ip_address))
  }, [logs.data, search])
  const paging = useSearchPaging(searched, logs, JSON.stringify({ filters, search }))

  return (
    <div className="space-y-6">
      <PageHeader title="Audit log" description="An append-only record of security-relevant actions in this organization. Secrets are never recorded." />
      <TableToolbar>
        <TableSearch value={search} onChange={setSearch} placeholder="Search actor, action, resource…" />
        <FilterSelect value={category} onChange={setCategory} label="Filter by category" allLabel="All activity" className="w-48" options={CATEGORIES} />
        <FilterSelect
          value={resourceType}
          onChange={setResourceType}
          label="Filter by resource type"
          allLabel="All resources"
          className="w-44"
          options={RESOURCE_TYPES.map((t) => ({ value: t, label: t.replace(/_/g, " ") }))}
        />
        <ClearFiltersButton
          show={filtered}
          onClear={() => {
            setCategory(ALL)
            setResourceType(ALL)
            setSearch("")
          }}
        />
      </TableToolbar>
      {logs.error && <ErrorState error={logs.error} retry={() => logs.refetch()} />}
      {logs.isPending ? (
        <TableSkeleton rows={8} columns={5} />
      ) : paging.props.total === 0 ? (
        <EmptyState
          icon={ScrollText}
          title={filtered ? "No audit events match these filters" : "No audit events"}
          description={filtered ? "Try a different category, resource or search." : "Actions like creating databases and running backups appear here."}
        />
      ) : (
        <div className="space-y-3">
          <div className="overflow-hidden rounded-xl border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-8 pl-4" />
                  <TableHead>Time</TableHead>
                  <TableHead>Actor</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead className="hidden md:table-cell">Resource</TableHead>
                  <TableHead className="hidden pr-4 lg:table-cell">IP address</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {paging.rows.map((l) => {
                  const href = resourceHref(l)
                  const expanded = open === l.id
                  const hasMeta = Object.keys(l.metadata ?? {}).length > 0
                  return (
                    <Fragment key={l.id}>
                      <TableRow className={cn(hasMeta && "cursor-pointer")} onClick={() => hasMeta && setOpen(expanded ? null : l.id)}>
                        <TableCell className="pl-4">
                          {hasMeta && <ChevronRight className={cn("size-4 text-muted-foreground transition-transform", expanded && "rotate-90")} />}
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          <RelativeTime date={l.created_at} />
                        </TableCell>
                        <TableCell>
                          {l.actor_type === "system" ? (
                            <Badge variant="secondary">System</Badge>
                          ) : (
                            <span className="flex items-center gap-1.5">
                              <span className="max-w-48 truncate">{l.actor_email ?? "Unknown"}</span>
                              {l.actor_type === "api_token" && <Badge variant="outline">API token</Badge>}
                            </span>
                          )}
                        </TableCell>
                        <TableCell>
                          <div className={cn(l.action.endsWith("failed") && "text-destructive")}>{humanizeAction(l.action)}</div>
                          <div className="font-mono text-xs text-muted-foreground">{l.action}</div>
                        </TableCell>
                        <TableCell className="hidden md:table-cell">
                          {l.resource_type ? (
                            href ? (
                              <Link href={href} className="hover:underline" onClick={(e) => e.stopPropagation()}>
                                {l.resource_type} <span className="font-mono text-xs text-muted-foreground">{shortId(l.resource_id)}</span>
                              </Link>
                            ) : (
                              <span>
                                {l.resource_type} <span className="font-mono text-xs text-muted-foreground">{shortId(l.resource_id)}</span>
                              </span>
                            )
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell className="hidden pr-4 font-mono text-xs text-muted-foreground lg:table-cell">{l.ip_address ?? "—"}</TableCell>
                      </TableRow>
                      {expanded && (
                        <TableRow className="hover:bg-transparent">
                          <TableCell colSpan={6} className="bg-muted/30 px-4 py-3">
                            <pre className="overflow-x-auto font-mono text-xs whitespace-pre-wrap">{JSON.stringify(l.metadata, null, 2)}</pre>
                            {l.user_agent && <p className="mt-2 truncate text-xs text-muted-foreground">User agent: {l.user_agent}</p>}
                          </TableCell>
                        </TableRow>
                      )}
                    </Fragment>
                  )
                })}
              </TableBody>
            </Table>
          </div>
          <TablePagination {...paging.props} noun="events" />
        </div>
      )}
    </div>
  )
}
