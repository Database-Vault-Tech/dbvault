import type { LucideIcon } from "lucide-react"
import type { ReactNode } from "react"

import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

export function StatCard({
  label,
  value,
  hint,
  icon: Icon,
  loading,
  tone,
}: {
  label: string
  value: ReactNode
  hint?: ReactNode
  icon: LucideIcon
  loading?: boolean
  tone?: "default" | "error" | "success"
}) {
  return (
    <Card className="gap-3 px-4 py-4">
      <div className="flex items-center justify-between text-sm text-muted-foreground">
        <span>{label}</span>
        <Icon className={cn("size-4", tone === "error" && "text-destructive", tone === "success" && "text-success")} />
      </div>
      {loading ? (
        <Skeleton className="h-8 w-20" />
      ) : (
        <div className={cn("text-3xl font-semibold tracking-tight tabular", tone === "error" && "text-destructive")}>{value}</div>
      )}
      {hint && <div className="text-xs text-muted-foreground">{hint}</div>}
    </Card>
  )
}
