// Formatting helpers shared by every page.

const UNITS = ["B", "KB", "MB", "GB", "TB", "PB"]

/** 1024-based byte formatting: 505413632 → "482 MB". */
export function formatBytes(bytes: number | null | undefined, digits = 1): string {
  if (bytes === null || bytes === undefined || Number.isNaN(bytes)) return "—"
  if (bytes < 1024) return `${bytes} B`
  let i = 0
  let n = bytes
  while (n >= 1024 && i < UNITS.length - 1) {
    n /= 1024
    i++
  }
  const s = n >= 100 ? n.toFixed(0) : n.toFixed(digits)
  return `${s.replace(/\.0$/, "")} ${UNITS[i]}`
}

/** Milliseconds → "2m 14s", "41s", "1h 03m". */
export function formatDuration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return "—"
  if (ms < 1000) return `${Math.max(0, Math.round(ms))}ms`
  const total = Math.round(ms / 1000)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  if (h > 0) return `${h}h ${String(m).padStart(2, "0")}m`
  if (m > 0) return `${m}m ${String(s).padStart(2, "0")}s`
  return `${s}s`
}

const rtf = new Intl.RelativeTimeFormat("en", { numeric: "auto" })
const DIVISIONS: { amount: number; unit: Intl.RelativeTimeFormatUnit }[] = [
  { amount: 60, unit: "second" },
  { amount: 60, unit: "minute" },
  { amount: 24, unit: "hour" },
  { amount: 7, unit: "day" },
  { amount: 4.34524, unit: "week" },
  { amount: 12, unit: "month" },
  { amount: Number.POSITIVE_INFINITY, unit: "year" },
]

/** "12 minutes ago", "in 3 hours". */
export function formatRelative(date: string | Date | null | undefined, now: Date = new Date()): string {
  if (!date) return "—"
  const d = typeof date === "string" ? new Date(date) : date
  let duration = (d.getTime() - now.getTime()) / 1000
  if (Math.abs(duration) < 10) return "just now"
  for (const div of DIVISIONS) {
    if (Math.abs(duration) < div.amount) return rtf.format(Math.round(duration), div.unit)
    duration /= div.amount
  }
  return d.toLocaleDateString()
}

const dateTime = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "medium" })
const timeOnly = new Intl.DateTimeFormat("en", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })

export function formatDateTime(date: string | Date | null | undefined): string {
  if (!date) return "—"
  return dateTime.format(typeof date === "string" ? new Date(date) : date)
}

export function formatTime(date: string | Date): string {
  return timeOnly.format(typeof date === "string" ? new Date(date) : date)
}

export function formatNumber(n: number | null | undefined): string {
  if (n === null || n === undefined) return "—"
  return new Intl.NumberFormat("en").format(n)
}

export function formatPercent(ratio: number | null | undefined): string {
  if (ratio === null || ratio === undefined) return "—"
  return `${(ratio * 100).toFixed(ratio >= 0.995 || ratio === 0 ? 0 : 1)}%`
}

export const storageLabels: Record<string, string> = {
  local: "Local disk",
  s3: "Amazon S3",
  r2: "Cloudflare R2",
  minio: "MinIO",
}

export function storageShort(type: string): string {
  return ({ local: "Disk", s3: "S3", r2: "R2", minio: "MinIO" } as Record<string, string>)[type] ?? type
}

export function shortId(id: string | null | undefined): string {
  return id ? id.slice(0, 8) : "—"
}

/** Human label for audit actions: "backup.completed" → "Backup completed". */
export function humanizeAction(action: string): string {
  const s = action.replace(/[._]/g, " ")
  return s.charAt(0).toUpperCase() + s.slice(1)
}
