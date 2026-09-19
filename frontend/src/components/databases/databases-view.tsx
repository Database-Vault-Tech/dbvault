"use client"

import { Database as DatabaseIcon, Plus } from "lucide-react"
import Link from "next/link"
import { useRouter } from "next/navigation"

import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { PageHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { StatusBadge, StatusDot } from "@/components/app/status"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { formatBytes } from "@/lib/format"
import { useOrg } from "@/lib/org"
import { useDatabases } from "@/lib/queries"
import type { Database } from "@/lib/types"

import { DatabaseRowActions } from "./database-actions"

function ConnectionState({ db }: { db: Database }) {
  const tone = db.last_test_ok === null ? "neutral" : db.last_test_ok ? "success" : "error"
  const label = db.last_test_ok === null ? "Not tested" : db.last_test_ok ? "Reachable" : "Unreachable"
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex items-center gap-1.5 text-sm text-muted-foreground">
          <StatusDot tone={tone} /> {label}
        </span>
      </TooltipTrigger>
      <TooltipContent>{db.last_test_error ?? (db.last_tested_at ? `Tested ${new Date(db.last_tested_at).toLocaleString()}` : "Run a connection test")}</TooltipContent>
    </Tooltip>
  )
}

export function DatabasesView() {
  const { can } = useOrg()
  const router = useRouter()
  const { data, isPending, error, refetch } = useDatabases()

  return (
    <div>
      <PageHeader
        title="Databases"
        description="PostgreSQL databases DBVault protects. Credentials are encrypted at rest and never returned by the API."
        actions={
          can("admin") && (
            <Button asChild>
              <Link href="/databases/new">
                <Plus /> Add database
              </Link>
            </Button>
          )
        }
      />
      {error ? (
        <ErrorState error={error} retry={() => refetch()} />
      ) : isPending ? (
        <TableSkeleton rows={4} columns={6} />
      ) : data.length === 0 ? (
        <EmptyState
          icon={DatabaseIcon}
          title="No databases yet."
          description="Connect your first PostgreSQL database and DBVault will start protecting it."
          action={
            can("admin") && (
              <Button asChild>
                <Link href="/databases/new">
                  <Plus /> Add Database
                </Link>
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
                <TableHead>Protection</TableHead>
                <TableHead className="hidden md:table-cell">Last backup</TableHead>
                <TableHead className="hidden lg:table-cell">Next run</TableHead>
                <TableHead className="hidden text-right sm:table-cell">Storage</TableHead>
                <TableHead className="hidden xl:table-cell">Connection</TableHead>
                <TableHead className="w-10 pr-4" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((db) => (
                <TableRow key={db.id} className="cursor-pointer" onClick={() => router.push(`/databases/${db.id}`)}>
                  <TableCell className="pl-4">
                    <Link href={`/databases/${db.id}`} className="font-medium hover:underline" onClick={(e) => e.stopPropagation()}>
                      {db.name}
                    </Link>
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                      <span className="max-w-64 truncate font-mono">
                        {db.host}:{db.port}/{db.database}
                      </span>
                      {db.pg_version && <span className="shrink-0 rounded border px-1 font-mono text-[10.5px]">PG {db.pg_version}</span>}
                    </div>
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={db.protected ? "healthy" : "unprotected"} label={db.protected ? "Protected" : "Unprotected"} />
                  </TableCell>
                  <TableCell className="hidden md:table-cell">
                    {db.last_backup_status ? (
                      <div className="flex items-center gap-2">
                        <StatusDot status={db.last_backup_status} />
                        <RelativeTime date={db.last_backup_at} className="text-sm text-muted-foreground" />
                      </div>
                    ) : (
                      <span className="text-sm text-muted-foreground">Never</span>
                    )}
                  </TableCell>
                  <TableCell className="hidden text-muted-foreground lg:table-cell">
                    {db.next_run_at ? <RelativeTime date={db.next_run_at} /> : "—"}
                  </TableCell>
                  <TableCell className="hidden text-right tabular sm:table-cell">{formatBytes(db.storage_bytes)}</TableCell>
                  <TableCell className="hidden xl:table-cell">
                    <ConnectionState db={db} />
                  </TableCell>
                  <TableCell className="pr-4 text-right">
                    <DatabaseRowActions db={db} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
