"use client"

import { History, Info, RotateCcw } from "lucide-react"
import { useSearchParams } from "next/navigation"
import { useState } from "react"

import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { PageHeader, SectionHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { StatusBadge } from "@/components/app/status"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatDateTime, formatDuration } from "@/lib/format"
import { useOrg } from "@/lib/org"
import { useRestores } from "@/lib/queries"

import { RestoreDetailDialog } from "./restore-detail"
import { RestoreWizardDialog } from "./restore-wizard"

export function RestoreView() {
  const { can } = useOrg()
  const params = useSearchParams()
  const { data, isPending, error, refetch } = useRestores()
  const [selected, setSelected] = useState<string | null>(null)
  const presetBackup = params.get("backup") ?? undefined
  // Arriving from a backup's "Restore" action opens the form right away.
  const [wizardOpen, setWizardOpen] = useState(!!presetBackup)
  const canRestore = can("admin")
  const newRestore = (
    <Button onClick={() => setWizardOpen(true)}>
      <RotateCcw /> New restore
    </Button>
  )

  return (
    <div className="space-y-8">
      <PageHeader
        title="Restore"
        description="Restore any completed backup into a new database or over an existing one. Every restore is tracked and audited."
        actions={canRestore ? newRestore : undefined}
      />
      {canRestore ? (
        <RestoreWizardDialog
          open={wizardOpen}
          onOpenChange={setWizardOpen}
          initialBackupId={presetBackup}
          onCreated={(id) => {
            setWizardOpen(false)
            setSelected(id)
          }}
        />
      ) : (
        <Alert>
          <Info />
          <AlertTitle>Restores require the admin role</AlertTitle>
          <AlertDescription>You can follow restore progress below. Ask an organization admin or owner to start a restore.</AlertDescription>
        </Alert>
      )}

      <section>
        <SectionHeader title="Restore history" description="Queued, running and past restores in this organization." />
        {error ? (
          <ErrorState error={error} retry={() => refetch()} />
        ) : isPending ? (
          <TableSkeleton rows={3} columns={6} />
        ) : data.length === 0 ? (
          <EmptyState
            icon={History}
            title="No restores yet"
            description="When you restore a backup, its progress, verification and logs appear here. Tip: restore into a new database regularly to rehearse disaster recovery."
            action={canRestore ? newRestore : undefined}
          />
        ) : (
          <div className="overflow-hidden rounded-xl border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="pl-4">Source</TableHead>
                  <TableHead className="hidden md:table-cell">Backup from</TableHead>
                  <TableHead>Target</TableHead>
                  <TableHead className="hidden sm:table-cell">Mode</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="hidden lg:table-cell">Requested by</TableHead>
                  <TableHead className="hidden text-right lg:table-cell">Duration</TableHead>
                  <TableHead className="pr-4 text-right">Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.map((r) => (
                  <TableRow key={r.id} className="cursor-pointer" onClick={() => setSelected(r.id)}>
                    <TableCell className="pl-4 font-medium">{r.source_database_name}</TableCell>
                    <TableCell className="hidden text-muted-foreground md:table-cell">{formatDateTime(r.backup_created_at)}</TableCell>
                    <TableCell>
                      <span className="font-mono text-xs">{r.mode === "new" ? r.new_database_name : r.target_database_name}</span>
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground sm:table-cell">{r.mode === "new" ? "New" : "Overwrite"}</TableCell>
                    <TableCell>
                      <StatusBadge status={r.status} />
                    </TableCell>
                    <TableCell className="hidden max-w-48 truncate text-muted-foreground lg:table-cell">{r.requested_by_email ?? "—"}</TableCell>
                    <TableCell className="hidden text-right tabular lg:table-cell">{formatDuration(r.duration_ms)}</TableCell>
                    <TableCell className="pr-4 text-right text-muted-foreground">
                      <RelativeTime date={r.created_at} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </section>
      <RestoreDetailDialog id={selected} onOpenChange={(o) => !o && setSelected(null)} />
    </div>
  )
}
