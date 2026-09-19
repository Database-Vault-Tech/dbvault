"use client"

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { formatBytes } from "@/lib/format"
import type { DayStat } from "@/lib/types"
import { cn } from "@/lib/utils"

/** 14-day backup activity: stacked completed/failed bars. */
export function ActivityChart({ days }: { days: DayStat[] }) {
  const max = Math.max(1, ...days.map((d) => d.completed + d.failed))
  return (
    <div className="flex h-36 items-end gap-1.5" role="img" aria-label="Backups per day over the last 14 days">
      {days.map((d) => {
        const total = d.completed + d.failed
        const date = new Date(d.date + "T00:00:00")
        return (
          <Tooltip key={d.date}>
            <TooltipTrigger asChild>
              <div className="group flex h-full flex-1 flex-col justify-end gap-px">
                {total === 0 ? (
                  <div className="h-1 rounded-sm bg-muted" />
                ) : (
                  <>
                    {d.failed > 0 && <div className="rounded-sm bg-destructive/80" style={{ height: `${(d.failed / max) * 100}%` }} />}
                    {d.completed > 0 && (
                      <div
                        className={cn("rounded-sm bg-brand/70 transition-colors group-hover:bg-brand", d.failed > 0 && "rounded-t-none")}
                        style={{ height: `${(d.completed / max) * 100}%` }}
                      />
                    )}
                  </>
                )}
              </div>
            </TooltipTrigger>
            <TooltipContent>
              <div className="font-medium">{date.toLocaleDateString("en", { weekday: "short", month: "short", day: "numeric" })}</div>
              <div>
                {d.completed} completed · {d.failed} failed
              </div>
              {d.bytes > 0 && <div className="opacity-70">{formatBytes(d.bytes)} stored</div>}
            </TooltipContent>
          </Tooltip>
        )
      })}
    </div>
  )
}
