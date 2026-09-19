import { Progress } from "@/components/ui/progress"
import { formatBytes } from "@/lib/format"
import type { BackupProgress } from "@/lib/types"

/**
 * Backup progress. pg_dump output size is only loosely related to the
 * database's on-disk size, so the bar is an estimate capped at 99% until the
 * job actually finishes.
 */
export function BackupProgressBar({ progress }: { progress: BackupProgress | null | undefined }) {
  if (!progress) {
    return (
      <div className="space-y-1.5">
        <Progress value={3} className="h-1.5" />
        <p className="text-xs text-muted-foreground">Starting…</p>
      </div>
    )
  }
  const pct =
    progress.phase === "verifying"
      ? 99
      : progress.estimated_total > 0
        ? Math.min(99, Math.round((progress.bytes_dumped / progress.estimated_total) * 100))
        : 50
  return (
    <div className="space-y-1.5">
      <Progress value={pct} className="h-1.5" />
      <p className="text-xs text-muted-foreground tabular">
        {progress.phase === "verifying"
          ? "Verifying uploaded checksum…"
          : `Dumped ${formatBytes(progress.bytes_dumped)} · uploaded ${formatBytes(progress.bytes_written)}`}
      </p>
    </div>
  )
}
