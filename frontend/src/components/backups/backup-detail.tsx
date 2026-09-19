"use client"

import { AlertOctagon, ArrowLeft, ChevronDown, Download, FileText, RotateCcw, RotateCw, ShieldCheck, Square, Trash2 } from "lucide-react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { useState } from "react"
import { toast } from "sonner"

import { ConfirmDialog } from "@/components/app/confirm-dialog"
import { CopyField } from "@/components/app/copy-button"
import { ErrorState } from "@/components/app/error-state"
import { BackupProgressBar } from "@/components/app/job-progress"
import { LogViewer } from "@/components/app/log-viewer"
import { PageHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { StatusBadge } from "@/components/app/status"
import { StorageBadge } from "@/components/app/storage-badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { backupDownloadUrl, errorMessage } from "@/lib/api"
import { engineMeta } from "@/lib/engines"
import { formatBytes, formatDateTime, formatDuration } from "@/lib/format"
import { useOrg } from "@/lib/org"
import { useBackup, useCancelJob, useCreateBackup, useDeleteBackup, useSystemStatus, useVerifyBackup } from "@/lib/queries"
import type { Backup } from "@/lib/types"

import { VerificationCard } from "./verification-card"

function Detail({ label, children, wide }: { label: string; children: React.ReactNode; wide?: boolean }) {
  return (
    <div className={wide ? "sm:col-span-2" : undefined}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 min-w-0 text-sm tabular">{children}</dd>
    </div>
  )
}

function FailedCard({ backup }: { backup: Backup }) {
  const router = useRouter()
  const { can } = useOrg()
  const retry = useCreateBackup()
  return (
    <Card className="border-destructive/30 bg-destructive/5 ring-destructive/20">
      <CardContent className="flex flex-col gap-4 sm:flex-row sm:items-start">
        <AlertOctagon className="size-6 shrink-0 text-destructive" />
        <div className="min-w-0 flex-1 space-y-2">
          <h2 className="font-semibold text-destructive">Backup failed.</h2>
          <dl className="space-y-1 text-sm">
            <div className="flex gap-2">
              <dt className="w-20 shrink-0 text-muted-foreground">Database:</dt>
              <dd className="font-medium">{backup.database_name}</dd>
            </div>
            <div className="flex gap-2">
              <dt className="w-20 shrink-0 text-muted-foreground">Reason:</dt>
              <dd className="min-w-0 break-words">{backup.error ?? "Unknown error"}</dd>
            </div>
          </dl>
          <div className="flex flex-wrap gap-2 pt-1">
            {can("member") && (
              <Button
                disabled={retry.isPending}
                onClick={() =>
                  retry.mutate(
                    {
                      database_id: backup.database_id,
                      storage_destination_id: backup.storage_destination_id,
                      compression: backup.compression,
                      encrypted: backup.encrypted,
                    },
                    {
                      onSuccess: (q) => {
                        toast.success("Backup restarted")
                        router.push(`/backups/${q.backup_id}`)
                      },
                      onError: (err) => toast.error("Couldn't retry backup", { description: errorMessage(err) }),
                    },
                  )
                }
              >
                {retry.isPending ? <Spinner /> : <RotateCw />} Retry Backup
              </Button>
            )}
            <Button variant="outline" onClick={() => document.getElementById("logs")?.scrollIntoView({ behavior: "smooth" })}>
              <FileText /> View Logs
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

export function BackupDetailView({ id }: { id: string }) {
  const router = useRouter()
  const { can } = useOrg()
  const { data, isPending, error, refetch } = useBackup(id)
  const verify = useVerifyBackup()
  const del = useDeleteBackup()
  const cancel = useCancelJob()
  const system = useSystemStatus()
  const [deleting, setDeleting] = useState(false)

  if (error) return <ErrorState error={error} retry={() => refetch()} title="Couldn't load this backup" />
  if (isPending) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-10 w-72" />
        <Skeleton className="h-40" />
        <Skeleton className="h-64" />
      </div>
    )
  }

  const { backup: b, job, logs, verification_job, verification_logs } = data
  const engine = engineMeta(b.engine)
  const active = b.status === "queued" || b.status === "running"
  const completed = b.status === "completed"
  const verifying = b.verification_status === "running" || (verification_job && (verification_job.status === "queued" || verification_job.status === "running"))
  const verifyUnavailable = system.data && !system.data.verification.available
  const ratio = b.raw_size_bytes && b.size_bytes ? b.raw_size_bytes / b.size_bytes : null

  return (
    <div className="space-y-6">
      <div>
        <Button variant="ghost" size="sm" asChild className="mb-4 -ml-2 text-muted-foreground">
          <Link href="/backups">
            <ArrowLeft /> Backups
          </Link>
        </Button>
        <PageHeader
          className="pb-0"
          title={
            <span className="flex items-center gap-3">
              <Link href={`/databases/${b.database_id}`} className="hover:underline">
                {b.database_name}
              </Link>{" "}
              backup
              <StatusBadge status={b.status} />
            </span>
          }
          description={
            <span className="flex flex-wrap gap-x-3">
              <span>
                Created <RelativeTime date={b.created_at} />
              </span>
              <span className="capitalize">{b.trigger}</span>
              {b.schedule_name && <span>Schedule: {b.schedule_name}</span>}
              {b.deleted_reason && <span>Deleted ({b.deleted_reason})</span>}
            </span>
          }
          actions={
            <>
              {active && b.job_id && can("member") && (
                <Button
                  variant="outline"
                  disabled={cancel.isPending || job?.cancel_requested}
                  onClick={() =>
                    cancel.mutate(b.job_id!, {
                      onSuccess: () => toast.success("Cancellation requested"),
                      onError: (err) => toast.error("Couldn't cancel", { description: errorMessage(err) }),
                    })
                  }
                >
                  {cancel.isPending ? <Spinner /> : <Square />} {job?.cancel_requested ? "Cancelling…" : "Cancel"}
                </Button>
              )}
              {can("member") && completed && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span>
                      <Button
                        variant="outline"
                        disabled={verify.isPending || !!verifying}
                        onClick={() =>
                          verify.mutate(b.id, {
                            onSuccess: () => toast.success("Verification queued", { description: `Restoring into a temporary ${engine.label}…` }),
                            onError: (err) => toast.error("Couldn't start verification", { description: errorMessage(err) }),
                          })
                        }
                      >
                        {verify.isPending || verifying ? <Spinner /> : <ShieldCheck />} Verify backup
                      </Button>
                    </span>
                  </TooltipTrigger>
                  {verifyUnavailable && <TooltipContent>Restore testing is unavailable on this server; only the checksum will be checked.</TooltipContent>}
                </Tooltip>
              )}
              {completed && can("member") && (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="outline">
                      <Download /> Download <ChevronDown className="opacity-60" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-72">
                    <DropdownMenuLabel className="text-xs text-muted-foreground">Download</DropdownMenuLabel>
                    <DropdownMenuItem asChild>
                      <a href={backupDownloadUrl(b.id, "raw")} download>
                        <div>
                          <div>Artifact as stored</div>
                          <div className="text-xs text-muted-foreground">{b.encrypted ? "Encrypted" : "Unencrypted"}, {b.compression} · {formatBytes(b.size_bytes)}</div>
                        </div>
                      </a>
                    </DropdownMenuItem>
                    {can("admin") && (
                      <>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem asChild>
                          <a href={backupDownloadUrl(b.id, "dump")} download>
                            <div>
                              <div>
                                Decrypted {engine.extension} for {engine.restoreTool}
                              </div>
                              <div className="text-xs text-muted-foreground">
                                Plain {engine.formatLabel} · {formatBytes(b.raw_size_bytes)}
                              </div>
                            </div>
                          </a>
                        </DropdownMenuItem>
                      </>
                    )}
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
              {completed && can("admin") && (
                <Button asChild>
                  <Link href={`/restore?backup=${b.id}`}>
                    <RotateCcw /> Restore
                  </Link>
                </Button>
              )}
              {can("admin") && !active && b.status !== "deleted" && (
                <Button variant="outline" size="icon" aria-label="Delete backup" onClick={() => setDeleting(true)}>
                  <Trash2 className="text-destructive" />
                </Button>
              )}
            </>
          }
        />
      </div>

      {active && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm">
              <Spinner className="text-info" /> {b.status === "queued" ? "Waiting for a worker…" : "Backup in progress"}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <BackupProgressBar progress={job?.progress} />
            <LogViewer logs={logs} live />
          </CardContent>
        </Card>
      )}

      {b.status === "failed" && <FailedCard backup={b} />}

      <div className="grid gap-6 lg:grid-cols-2">
        <Card className="h-fit">
          <CardHeader>
            <CardTitle className="text-sm">Details</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-2 gap-x-6 gap-y-4">
              <Detail label="Size">{formatBytes(b.size_bytes)}</Detail>
              <Detail label="Raw dump size">{formatBytes(b.raw_size_bytes)}</Detail>
              <Detail label="Compression">
                <span className="font-mono">{b.compression}</span>
                {ratio && <span className="text-muted-foreground"> · {ratio.toFixed(1)}×</span>}
              </Detail>
              <Detail label="Encryption">{b.encrypted ? "age · X25519" : "Not encrypted"}</Detail>
              <Detail label="Duration">{formatDuration(b.duration_ms)}</Detail>
              <Detail label="Storage">
                <StorageBadge type={b.storage_type} name={b.storage_name} />
              </Detail>
              <Detail label={engine.label}>{b.pg_version ?? "—"}</Detail>
              <Detail label="Dump tool">
                {!b.pg_dump_version ? "—" : /^[a-z]/i.test(b.pg_dump_version) ? b.pg_dump_version : `${engine.dumpTool} ${b.pg_dump_version}`}
              </Detail>
              <Detail label="Tables">{b.table_count ?? "—"}</Detail>
              <Detail label="Format">{b.format === "pg_dump_custom" || b.format === "sql" ? engine.formatLabel : b.format}</Detail>
              <Detail label="Started">{formatDateTime(b.started_at)}</Detail>
              <Detail label="Completed">{formatDateTime(b.completed_at)}</Detail>
              {b.storage_key && (
                <Detail label="Storage key" wide>
                  <CopyField value={b.storage_key} />
                </Detail>
              )}
              {b.checksum_sha256 && (
                <Detail label="Checksum (SHA-256)" wide>
                  <CopyField value={b.checksum_sha256} />
                </Detail>
              )}
              {b.job_id && (
                <Detail label="Job ID" wide>
                  <span className="font-mono text-xs text-muted-foreground">{b.job_id}</span>
                </Detail>
              )}
            </dl>
          </CardContent>
        </Card>
        {completed || b.verification ? (
          <VerificationCard backup={b} job={verification_job} logs={verification_logs} />
        ) : (
          <Card className="h-fit">
            <CardHeader>
              <CardTitle className="text-sm">Verification</CardTitle>
            </CardHeader>
            <CardContent className="text-sm text-muted-foreground">Verification becomes available once the backup completes.</CardContent>
          </Card>
        )}
      </div>

      <Card id="logs" className="scroll-mt-20">
        <CardHeader>
          <CardTitle className="text-sm">Backup log</CardTitle>
        </CardHeader>
        <CardContent>
          <LogViewer logs={logs} live={active} />
        </CardContent>
      </Card>

      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title="Delete this backup?"
        confirmLabel="Delete backup"
        pending={del.isPending}
        description={
          <>
            <p>The artifact is permanently removed from {b.storage_name}. The record stays in history as deleted.</p>
            <p>This can&apos;t be undone.</p>
          </>
        }
        onConfirm={() =>
          del.mutate(b.id, {
            onSuccess: () => {
              toast.success("Backup deleted")
              setDeleting(false)
              router.push("/backups")
            },
            onError: (err) => toast.error("Couldn't delete backup", { description: errorMessage(err) }),
          })
        }
      />
    </div>
  )
}
