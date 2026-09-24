"use client"

import { Users } from "lucide-react"
import Link from "next/link"
import { useMemo, useState } from "react"

import { matches, TablePagination, TableSearch, TableToolbar, usePaging } from "@/components/app/data-table"
import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { RelativeTime } from "@/components/app/relative-time"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useAdminUsers } from "@/lib/queries"

import { AdminShell } from "./admin-shell"

export function AdminUsers() {
  const { data, isPending, error, refetch } = useAdminUsers()
  const [search, setSearch] = useState("")
  const rows = useMemo(
    () => (data ?? []).filter((u) => matches(search, u.name, u.email, ...u.organizations.map((o) => o.name))),
    [data, search],
  )
  const paging = usePaging(rows, search)

  return (
    <AdminShell>
      {!isPending && !error && (data?.length ?? 0) > 0 && (
        <TableToolbar>
          <TableSearch value={search} onChange={setSearch} placeholder="Search name, email, organization…" />
        </TableToolbar>
      )}
      {error ? (
        <ErrorState error={error} retry={() => refetch()} />
      ) : isPending ? (
        <TableSkeleton rows={5} columns={4} />
      ) : rows.length === 0 ? (
        <EmptyState icon={Users} title="No users match" description="Try a different search." />
      ) : (
        <div className="space-y-3">
          <div className="overflow-hidden rounded-xl border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="pl-4">User</TableHead>
                  <TableHead>Organizations</TableHead>
                  <TableHead className="hidden md:table-cell">Last sign-in</TableHead>
                  <TableHead className="hidden pr-4 lg:table-cell">Joined</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {paging.rows.map((u) => (
                  <TableRow key={u.id}>
                    <TableCell className="pl-4">
                      <div className="flex items-center gap-2 font-medium">
                        {u.name}
                        {u.is_instance_admin && <Badge variant="outline">Instance admin</Badge>}
                      </div>
                      <div className="max-w-72 truncate text-xs text-muted-foreground">{u.email}</div>
                    </TableCell>
                    <TableCell>
                      {u.organizations.length === 0 ? (
                        <span className="text-sm text-muted-foreground">None</span>
                      ) : (
                        <div className="flex max-w-md flex-wrap gap-1">
                          {u.organizations.map((o) => (
                            <Link key={o.id} href={`/admin/organizations/${o.id}`}>
                              <Badge variant="secondary" className="font-normal hover:bg-secondary/70">
                                {o.name} <span className="text-muted-foreground capitalize">· {o.role}</span>
                              </Badge>
                            </Link>
                          ))}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground md:table-cell">
                      {u.last_login_at ? <RelativeTime date={u.last_login_at} /> : "Never"}
                    </TableCell>
                    <TableCell className="hidden pr-4 text-muted-foreground lg:table-cell">
                      <RelativeTime date={u.created_at} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <TablePagination {...paging.props} noun="users" />
        </div>
      )}
    </AdminShell>
  )
}
