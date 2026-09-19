"use client"

import { ArrowRight } from "lucide-react"
import Link from "next/link"
import { useRouter } from "next/navigation"

import { RelativeTime } from "@/components/app/relative-time"
import { StatusBadge } from "@/components/app/status"
import { StorageBadge } from "@/components/app/storage-badge"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatBytes, formatDuration } from "@/lib/format"
import type { Backup } from "@/lib/types"

export function BackupsTable({ backups, showDatabase = true }: { backups: Backup[]; showDatabase?: boolean }) {
  const router = useRouter()
  return (
    <div className="overflow-hidden rounded-xl border bg-card">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            {showDatabase && <TableHead className="pl-4">Database</TableHead>}
            <TableHead className={showDatabase ? "" : "pl-4"}>Status</TableHead>
            <TableHead className="text-right">Size</TableHead>
            <TableHead className="hidden text-right sm:table-cell">Duration</TableHead>
            <TableHead className="hidden md:table-cell">Storage</TableHead>
            <TableHead className="hidden lg:table-cell">Trigger</TableHead>
            <TableHead className="pr-4 text-right">Created</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {backups.map((b) => (
            <TableRow key={b.id} className="cursor-pointer" onClick={() => router.push(`/backups/${b.id}`)}>
              {showDatabase && (
                <TableCell className="pl-4 font-medium">
                  <Link href={`/backups/${b.id}`} className="hover:underline" onClick={(e) => e.stopPropagation()}>
                    {b.database_name}
                  </Link>
                </TableCell>
              )}
              <TableCell className={showDatabase ? "" : "pl-4"}>
                <div className="flex items-center gap-1.5">
                  <StatusBadge status={b.status} />
                  {b.verification_status === "passed" && <StatusBadge status="passed" tone="success" label="Verified" className="hidden xl:inline-flex" />}
                </div>
              </TableCell>
              <TableCell className="text-right tabular">{formatBytes(b.size_bytes)}</TableCell>
              <TableCell className="hidden text-right tabular sm:table-cell">{formatDuration(b.duration_ms)}</TableCell>
              <TableCell className="hidden md:table-cell">
                <StorageBadge type={b.storage_type} name={b.storage_name} />
              </TableCell>
              <TableCell className="hidden text-muted-foreground capitalize lg:table-cell">{b.trigger}</TableCell>
              <TableCell className="pr-4 text-right text-muted-foreground">
                <RelativeTime date={b.created_at} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

export function RecentBackups({ backups }: { backups: Backup[] }) {
  return (
    <div className="space-y-3">
      <div className="flex items-end justify-between">
        <div>
          <h2 className="text-sm font-semibold">Recent backups</h2>
          <p className="text-sm text-muted-foreground">The latest backup jobs across every database.</p>
        </div>
        <Button variant="ghost" size="sm" asChild>
          <Link href="/backups">
            View all <ArrowRight />
          </Link>
        </Button>
      </div>
      <BackupsTable backups={backups} />
    </div>
  )
}
