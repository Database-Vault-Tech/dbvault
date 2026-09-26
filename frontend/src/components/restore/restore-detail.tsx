"use client"

import { AlertTriangle, ArrowDown, Check, CheckCircle2, EyeOff, Loader2, RotateCcw, Square, X, XCircle } from "lucide-react"
import type { ReactNode } from "react"
import { toast } from "sonner"

import { LogViewer } from "@/components/app/log-viewer"
import { RelativeTime } from "@/components/app/relative-time"
import { StatusBadge } from "@/components/app/status"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { errorMessage } from "@/lib/api"
import { engineMeta } from "@/lib/engines"
import { formatDateTime, formatDuration, formatNumber } from "@/lib/format"
import { useOrg } from "@/lib/org"
import { useCancelJob, useRestore } from "@/lib/queries"
import type { RestoreJob, RestoreStatus } from "@/lib/types"
import { cn } from "@/lib/utils"

type Step = { key: string; label: string; hint: string }
const STEPS: Step[] = [
  { key: "queued", label: "Queued", hint: "Waiting for a worker" },
  { key: "running", label: "Restoring", hint: "Download, verify, restore" },
  { key: "verifying", label: "Verifying", hint: "Checking restored tables" },
  { key: "completed", label: "Completed", hint: "Ready to use" },
]
// Masked restores anonymize in a sandbox before restoring into the target.
const MASKED_STEPS: Step[] = [
  STEPS[0],
  { key: "masking", label: "Masking", hint: "Sandbox restore, mask, check" },
  { key: "running", label: "Restoring", hint: "Masked copy into the target" },
  STEPS[2],
  STEPS[3],
]
const ACTIVE = new Set(["queued", "running", "masking", "verifying"])

/** Index of the step a restore is on. Masked restores are "running" twice:
 * before masking (sandbox restore) and after (into the target). */
function stepIndex(r: RestoreJob): number {
  if (!r.masking_profile) return { queued: 0, running: 1, verifying: 2, completed: 3 }[r.status as string] ?? 0
  if (r.status === "running") return r.masking_report ? 2 : 1
  return { queued: 0, masking: 1, verifying: 3, completed: 4 }[r.status as string] ?? 0
}

function Stepper({ status, current: at, failedAt, steps }: { status: RestoreStatus; current: number; failedAt: number; steps: Step[] }) {
  const failed = status === "failed" || status === "cancelled"
  const current = failed ? failedAt : at
  return (
    <ol className={cn("grid grid-cols-2 gap-x-3 gap-y-4", steps.length === 5 ? "sm:grid-cols-5" : "sm:grid-cols-4")}>
      {steps.map((s, i) => {
        const done = status === "completed" || (!failed && i < current) || (failed && i < failedAt)
        const active = !failed && i === current && status !== "completed"
        const broken = failed && i === failedAt
        return (
          <li key={s.key} className="relative flex items-start gap-2.5">
            <span
              className={cn(
                "flex size-7 shrink-0 items-center justify-center rounded-full border text-xs font-medium",
                done && "border-success bg-success text-background",
                active && "border-info bg-info/10 text-info",
                broken && "border-destructive bg-destructive text-background",
                !done && !active && !broken && "text-muted-foreground",
              )}
            >
              {done ? <Check className="size-4" /> : active ? <Spinner className="size-3.5" /> : broken ? <X className="size-4" /> : i + 1}
            </span>
            <span className="min-w-0 pt-0.5">
              <span className={cn("block text-sm", active || broken ? "font-medium" : done ? "" : "text-muted-foreground", broken && "text-destructive")}>
                {broken ? (status === "cancelled" ? "Cancelled" : "Failed") : s.label}
              </span>
              <span className="block text-xs text-muted-foreground">{s.hint}</span>
            </span>
          </li>
        )
      })}
    </ol>
  )
}

function Endpoint({ label, title, detail, tone }: { label: string; title: string; detail: string; tone?: "brand" | "danger" }) {
  return (
    <div
      className={cn(
        "min-w-0 rounded-lg border px-3.5 py-3",
        tone === "brand" && "border-brand/40 bg-brand/5",
        tone === "danger" && "border-destructive/40 bg-destructive/5",
      )}
    >
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="truncate font-mono text-sm font-medium" title={title}>
        {title}
      </div>
      <div className="truncate text-xs text-muted-foreground" title={detail}>
        {detail}
      </div>
    </div>
  )
}

function Detail({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="truncate text-sm">{children}</dd>
    </div>
  )
}

function HeaderIcon({ status }: { status: RestoreStatus }) {
  const base = "flex size-10 shrink-0 items-center justify-center rounded-xl"
  if (status === "completed")
    return (
      <span className={cn(base, "bg-success/10 text-success")}>
        <CheckCircle2 className="size-5" />
      </span>
    )
  if (status === "failed" || status === "cancelled")
    return (
      <span className={cn(base, "bg-destructive/10 text-destructive")}>
        <XCircle className="size-5" />
      </span>
    )
  return (
    <span className={cn(base, "bg-brand/10 text-brand")}>
      <Loader2 className="size-5 animate-spin" />
    </span>
  )
}

function headline(r: RestoreJob): string {
  const target = r.mode === "new" ? r.new_database_name : r.target_database_name
  switch (r.status) {
    case "completed":
      return `Restored into ${target}`
    case "failed":
      return `Restore into ${target} failed`
    case "cancelled":
      return `Restore into ${target} cancelled`
    default:
      return `Restoring into ${target}`
  }
}

/** Large centered dialog following a restore job from queue to completion. */
export function RestoreDetailDialog({ id, onOpenChange }: { id: string | null; onOpenChange: (open: boolean) => void }) {
  const { can } = useOrg()
  const { data, isPending } = useRestore(id)
  const cancel = useCancelJob()
  const r = data?.restore
  const active = !!r && ACTIVE.has(r.status)
  // Where a failed restore stopped: masking, restoring, or verifying.
  const failedAt = !r ? 1 : r.masking_profile ? (r.verification ? 3 : r.masking_report ? 2 : 1) : r.verification ? 2 : 1

  return (
    <Dialog open={!!id} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl">
        {isPending || !r ? (
          <div className="space-y-4 p-6">
            <DialogTitle className="sr-only">Restore</DialogTitle>
            <DialogDescription className="sr-only">Loading restore details</DialogDescription>
            <Skeleton className="h-10 w-2/3" />
            <Skeleton className="h-16" />
            <Skeleton className="h-64" />
          </div>
        ) : (
          <>
            <div className="flex items-start gap-3 border-b px-6 py-5 pr-12">
              <HeaderIcon status={r.status} />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <DialogTitle className="truncate text-lg">{headline(r)}</DialogTitle>
                  <StatusBadge status={r.status} />
                </div>
                <DialogDescription className="mt-0.5">
                  Backup of <span className="font-medium text-foreground">{r.source_database_name}</span> taken{" "}
                  {formatDateTime(r.backup_created_at)}
                </DialogDescription>
              </div>
            </div>

            <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-6 py-5">
              <Stepper status={r.status} current={stepIndex(r)} failedAt={failedAt} steps={r.masking_profile ? MASKED_STEPS : STEPS} />
              {r.masking_profile && <MaskingSummary r={r} />}

              {r.error && (
                <Alert variant="destructive">
                  <AlertTriangle />
                  <AlertTitle>
                    {engineMeta(r.engine).atomicRestore || r.mode === "new"
                      ? "Restore failed — the target database was left unchanged"
                      : "Restore failed — the target database may be partially restored"}
                  </AlertTitle>
                  <AlertDescription className="font-mono text-xs break-words">{r.error}</AlertDescription>
                </Alert>
              )}
              {r.status === "completed" && r.verification && (
                <Alert className="border-success/30 bg-success/5">
                  <CheckCircle2 className="text-success" />
                  <AlertTitle className="text-success">Restore verified</AlertTitle>
                  <AlertDescription>
                    {formatNumber(r.verification.tables_found)} of {formatNumber(r.verification.tables_expected)} tables from the backup are present in{" "}
                    <span className="font-mono text-foreground">{r.mode === "new" ? r.new_database_name : r.target_database_name}</span>.
                  </AlertDescription>
                </Alert>
              )}

              <div className="grid gap-6 lg:grid-cols-[minmax(0,320px)_minmax(0,1fr)]">
                <div className="min-w-0 space-y-5">
                  <div className="space-y-2">
                    <Endpoint label="From backup" title={r.source_database_name} detail={formatDateTime(r.backup_created_at)} />
                    <div className="flex justify-center text-muted-foreground" aria-hidden>
                      <ArrowDown className="size-4" />
                    </div>
                    <Endpoint
                      label={`${r.mode === "new" ? "Into new" : "Over existing"} ${engineMeta(r.engine).fileBased ? "file" : "database"}`}
                      title={r.mode === "new" ? (r.new_database_name ?? "—") : r.target_database_name}
                      detail={
                        r.mode === "new"
                          ? engineMeta(r.engine).fileBased
                            ? "in the SQLite folder"
                            : `on ${r.target_database_name}'s server`
                          : engineMeta(r.engine).fileBased
                            ? "file replaced atomically"
                            : engineMeta(r.engine).atomicRestore
                            ? "objects replaced in one transaction"
                            : "objects dropped and recreated"
                      }
                      tone={r.mode === "new" ? "brand" : "danger"}
                    />
                  </div>
                  <dl className="grid grid-cols-2 gap-x-4 gap-y-3 rounded-lg border p-3.5">
                    <Detail label="Requested by">{r.requested_by_email ?? "—"}</Detail>
                    <Detail label="Started">
                      <RelativeTime date={r.started_at ?? r.created_at} />
                    </Detail>
                    <Detail label="Duration">
                      <span className="tabular">{active ? "In progress" : formatDuration(r.duration_ms)}</span>
                    </Detail>
                    <Detail label="Mode">{r.mode === "new" ? "New database" : "Overwrite"}</Detail>
                  </dl>
                </div>
                <div className="flex min-w-0 flex-col gap-2">
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium">Log</span>
                    {active && (
                      <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <span className="size-1.5 animate-pulse-dot rounded-full bg-info" /> Live
                      </span>
                    )}
                  </div>
                  <LogViewer logs={data.logs} live={active} className="max-h-none min-h-72 flex-1 lg:h-96" />
                </div>
              </div>
            </div>

            <div className="flex flex-col-reverse gap-2 border-t bg-muted/40 px-6 py-3.5 sm:flex-row sm:items-center sm:justify-between">
              <p className="text-xs text-muted-foreground">
                <RotateCcw className="mr-1 inline size-3" />
                Restores run in a single transaction: they either fully succeed or change nothing.
              </p>
              <div className="flex gap-2 sm:justify-end">
                {active && r.job_id && can("member") && (
                  <Button
                    variant="outline"
                    disabled={cancel.isPending || data?.job?.cancel_requested}
                    onClick={() =>
                      cancel.mutate(r.job_id!, {
                        onSuccess: () => toast.success("Cancellation requested", { description: "The transaction will roll back." }),
                        onError: (err) => toast.error("Couldn't cancel", { description: errorMessage(err) }),
                      })
                    }
                  >
                    {cancel.isPending ? <Spinner /> : <Square />} {data?.job?.cancel_requested ? "Cancelling…" : "Cancel restore"}
                  </Button>
                )}
                <Button variant={active ? "ghost" : "default"} onClick={() => onOpenChange(false)}>
                  Close
                </Button>
              </div>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}

/** What masking did (never data): rules per table and the checks that passed. */
function MaskingSummary({ r }: { r: RestoreJob }) {
  const rep = r.masking_report
  return (
    <div className="rounded-lg border border-brand/40 bg-brand/5 p-4">
      <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
        <EyeOff className="size-4 text-brand" /> Personal data masked
        <span className="font-normal text-muted-foreground">
          profile <span className="font-mono">{r.masking_profile}</span>
          {r.masking_profile_version ? ` v${r.masking_profile_version}` : ""}
        </span>
      </div>
      {rep ? (
        <>
          <p className="mt-1 text-sm text-muted-foreground">
            {formatNumber(rep.rows_changed)} rows changed in a sandbox and {rep.checks_passed} checks passed before anything reached {r.target_database_name}.
          </p>
          <ul className="mt-3 grid gap-1.5 text-xs sm:grid-cols-2">
            {rep.tables.map((t) => (
              <li key={t.table} className="min-w-0">
                <span className="font-mono text-foreground">{t.table}</span>{" "}
                <span className="text-muted-foreground">
                  {t.action === "truncated" ? `emptied (${formatNumber(t.rows)} rows)` : `${formatNumber(t.rows)} rows · ${t.columns?.join(", ")}`}
                </span>
              </li>
            ))}
          </ul>
        </>
      ) : (
        <p className="mt-1 text-sm text-muted-foreground">The backup is restored into a sandbox and masked there; real values never reach {r.target_database_name}.</p>
      )}
    </div>
  )
}
