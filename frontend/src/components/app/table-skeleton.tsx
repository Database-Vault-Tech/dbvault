import { Skeleton } from "@/components/ui/skeleton"

export function TableSkeleton({ rows = 5, columns = 5 }: { rows?: number; columns?: number }) {
  return (
    <div className="divide-y rounded-xl border bg-card">
      {Array.from({ length: rows }).map((_, r) => (
        <div key={r} className="flex items-center gap-4 px-4 py-3.5">
          {Array.from({ length: columns }).map((_, c) => (
            <Skeleton key={c} className="h-4" style={{ width: c === 0 ? "22%" : `${10 + ((r + c) % 3) * 4}%` }} />
          ))}
        </div>
      ))}
    </div>
  )
}
