import { cn } from "@/lib/utils"

export type Tone = "success" | "warning" | "error" | "running" | "queued" | "neutral"

const TONE_BY_STATUS: Record<string, Tone> = {
  completed: "success",
  passed: "success",
  pass: "success",
  delivered: "success",
  healthy: "success",
  ok: "success",
  running: "running",
  verifying: "running",
  masking: "running",
  queued: "queued",
  pending: "queued",
  failed: "error",
  fail: "error",
  critical: "error",
  cancelled: "neutral",
  none: "neutral",
  skipped: "neutral",
  deleted: "neutral",
  unavailable: "warning",
  warning: "warning",
  unprotected: "warning",
}

const LABELS: Record<string, string> = {
  none: "Not verified",
  passed: "Verified",
  pass: "Pass",
  fail: "Fail",
  unavailable: "Unavailable",
  masking: "Masking",
}

// Flat tinted pills, matching the landing page's "Available" / "Coming soon" chips.
const TONE_CLASSES: Record<Tone, { badge: string; dot: string }> = {
  success: { badge: "bg-success/15 text-success", dot: "bg-success" },
  warning: { badge: "bg-warning/15 text-warning", dot: "bg-warning" },
  error: { badge: "bg-destructive/15 text-destructive", dot: "bg-destructive" },
  running: { badge: "bg-info/15 text-info", dot: "bg-info animate-pulse-dot" },
  queued: { badge: "bg-muted text-muted-foreground", dot: "bg-muted-foreground/70" },
  neutral: { badge: "bg-muted text-muted-foreground", dot: "bg-muted-foreground/50" },
}

export function toneFor(status: string | null | undefined): Tone {
  return (status && TONE_BY_STATUS[status]) || "neutral"
}

export function StatusDot({ status, tone, className }: { status?: string | null; tone?: Tone; className?: string }) {
  const t = tone ?? toneFor(status)
  return <span aria-hidden className={cn("inline-block size-1.5 shrink-0 rounded-full", TONE_CLASSES[t].dot, className)} />
}

/** Consistent status pill used for backups, jobs, restores, checks and health. */
export function StatusBadge({
  status,
  label,
  tone,
  className,
}: {
  status: string | null | undefined
  label?: string
  tone?: Tone
  className?: string
}) {
  const t = tone ?? toneFor(status)
  const text = label ?? (status ? (LABELS[status] ?? status.charAt(0).toUpperCase() + status.slice(1)) : "—")
  return (
    <span className={cn("inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap", TONE_CLASSES[t].badge, className)}>
      {/* Only live states get a (pulsing) dot, so "running" stands out. */}
      {t === "running" && <StatusDot tone={t} />}
      {text}
    </span>
  )
}
