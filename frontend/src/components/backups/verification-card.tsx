"use client"

import { AlertTriangle, ShieldCheck } from "lucide-react"

import { LogViewer } from "@/components/app/log-viewer"
import { RelativeTime } from "@/components/app/relative-time"
import { StatusBadge } from "@/components/app/status"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { engineLabel } from "@/lib/engines"
import { formatDuration, formatNumber } from "@/lib/format"
import type { Backup, Job, LogEntry, VerificationCheck } from "@/lib/types"

const steps = (engine: string) => [
  "Download",
  "Checksum",
  "Decrypt",
  "Decompress",
  `Restore into a temporary ${engineLabel(engine)}`,
  "Run verification queries",
  "Destroy sandbox",
]

function CheckRow({ label, check }: { label: string; check: VerificationCheck }) {
  return (
    <div className="flex flex-col gap-1 py-3 sm:flex-row sm:items-start sm:justify-between sm:gap-6">
      <div className="min-w-0">
        <div className="text-sm font-medium">{label}</div>
        {check.message && <div className="text-xs text-muted-foreground">{check.message}</div>}
      </div>
      <StatusBadge status={check.status} label={check.status.toUpperCase()} className="shrink-0 self-start font-mono" />
    </div>
  )
}

export function VerificationCard({ backup, job, logs }: { backup: Backup; job?: Job; logs?: LogEntry[] }) {
  const running = backup.verification_status === "running" || (job && (job.status === "queued" || job.status === "running"))
  const r = backup.verification
  const STEPS = steps(backup.engine)
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-sm">
          <ShieldCheck className="size-4 text-muted-foreground" /> Verification
          {!running && <StatusBadge status={backup.verification_status} className="ml-auto" />}
        </CardTitle>
        <CardDescription>
          {backup.verified_at ? (
            <>
              Last proven restorable <RelativeTime date={backup.verified_at} />.
            </>
          ) : (
            "A backup you haven't restored is just a hope."
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {running && (
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Progress value={job?.status === "queued" ? 5 : 50} className="h-1.5 [&>*]:animate-pulse" />
              <p className="text-xs text-muted-foreground">{job?.status === "queued" ? "Waiting for a worker…" : "Restore test in progress…"}</p>
            </div>
            <LogViewer logs={logs} live className="max-h-72" />
          </div>
        )}
        {!running && !r && (
          <div className="space-y-2 text-sm text-muted-foreground">
            <p>Not verified yet. Verify runs a full recovery drill:</p>
            <ol className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs">
              {STEPS.map((s, i) => (
                <li key={s} className="flex items-center gap-1.5">
                  <span className="rounded border bg-muted/50 px-1.5 py-0.5">{s}</span>
                  {i < STEPS.length - 1 && <span aria-hidden>→</span>}
                </li>
              ))}
            </ol>
          </div>
        )}
        {!running && r && (
          <>
            {backup.verification_status === "unavailable" && (
              <Alert className="border-warning/40 bg-warning/5">
                <AlertTriangle className="text-warning" />
                <AlertTitle>Restore testing unavailable in this environment</AlertTitle>
                <AlertDescription>
                  <p>{r.restore.message}</p>
                  <p>
                    The checksum was verified, but the backup has not been proven restorable. Set <code className="font-mono">VERIFY_MODE=docker</code> or{" "}
                    <code className="font-mono">VERIFY_MODE=server</code> on the worker (see docs/restore.md).
                  </p>
                </AlertDescription>
              </Alert>
            )}
            <div className="divide-y">
              <CheckRow label="Backup integrity" check={r.integrity} />
              <CheckRow label="Restore test" check={r.restore} />
              <CheckRow label="Database verification" check={r.database} />
            </div>
            <dl className="grid grid-cols-2 gap-3 rounded-lg border bg-muted/30 p-3 text-sm sm:grid-cols-4">
              <div>
                <dt className="text-xs text-muted-foreground">Recovery test duration</dt>
                <dd className="font-medium tabular">{formatDuration(r.duration_ms)}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">Tables restored</dt>
                <dd className="font-medium tabular">
                  {r.tables_restored} / {r.tables_expected}
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">Rows counted</dt>
                <dd className="font-medium tabular">{formatNumber(r.rows)}</dd>
              </div>
              <div className="col-span-2 sm:col-span-1">
                <dt className="text-xs text-muted-foreground">Sandbox</dt>
                <dd className="truncate text-xs" title={r.sandbox}>
                  {r.sandbox || "—"}
                </dd>
              </div>
            </dl>
            {logs && logs.length > 0 && (
              <details className="group">
                <summary className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">Show verification log</summary>
                <LogViewer logs={logs} className="mt-2 max-h-72" />
              </details>
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}
