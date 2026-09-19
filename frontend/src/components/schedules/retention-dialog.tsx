"use client"

import { ShieldCheck } from "lucide-react"

import { ErrorState } from "@/components/app/error-state"
import { StatusBadge } from "@/components/app/status"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatDateTime } from "@/lib/format"
import { useRetentionPreview } from "@/lib/queries"
import type { Schedule } from "@/lib/types"

export function RetentionDialog({ schedule, onOpenChange }: { schedule?: Schedule; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={!!schedule} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Retention preview</DialogTitle>
          <DialogDescription>
            What the policy would keep and delete right now for {schedule?.database_name} · {schedule?.description}.
          </DialogDescription>
        </DialogHeader>
        {schedule && <Preview id={schedule.id} />}
      </DialogContent>
    </Dialog>
  )
}

function Preview({ id }: { id: string }) {
  const q = useRetentionPreview(id)
  if (q.error) return <ErrorState error={q.error} retry={() => q.refetch()} />
  if (!q.data) return <Skeleton className="h-40" />
  const p = q.data
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-3 gap-3 text-sm">
        <div className="rounded-lg border p-3">
          <div className="text-xs text-muted-foreground">Policy</div>
          <div className="font-medium">{p.summary}</div>
        </div>
        <div className="rounded-lg border p-3">
          <div className="text-xs text-muted-foreground">Kept</div>
          <div className="text-xl font-semibold tabular">{p.keep}</div>
        </div>
        <div className="rounded-lg border p-3">
          <div className="text-xs text-muted-foreground">Would be deleted</div>
          <div className="text-xl font-semibold tabular">{p.delete}</div>
        </div>
      </div>
      <div className="flex gap-2.5 rounded-lg border bg-muted/30 p-3 text-xs text-muted-foreground">
        <ShieldCheck className="size-4 shrink-0 text-success" />
        <ul className="space-y-0.5">
          <li>Nothing is deleted before the policy is re-evaluated by the cleanup worker.</li>
          <li>The latest backup and backups younger than 1 hour are always kept.</li>
          <li>Manual backups are never deleted automatically. A 0/0/0 policy keeps everything.</li>
        </ul>
      </div>
      {p.decisions.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">This schedule hasn&apos;t produced any completed backups yet.</p>
      ) : (
        <div className="overflow-hidden rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">Backup</TableHead>
                <TableHead>Decision</TableHead>
                <TableHead className="pr-4">Reason</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {p.decisions.map((d) => (
                <TableRow key={d.id}>
                  <TableCell className="pl-4 tabular">{formatDateTime(d.created_at)}</TableCell>
                  <TableCell>
                    <StatusBadge status={d.keep ? "pass" : "fail"} label={d.keep ? "Keep" : "Delete"} />
                  </TableCell>
                  <TableCell className="pr-4 text-xs whitespace-normal text-muted-foreground">{d.reasons.join(", ")}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
