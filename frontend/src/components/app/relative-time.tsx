"use client"

import { useEffect, useState } from "react"

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { formatDateTime, formatRelative } from "@/lib/format"

/** "12 minutes ago" that stays fresh, with the exact time on hover. */
export function RelativeTime({ date, className }: { date: string | null | undefined; className?: string }) {
  const [, tick] = useState(0)
  useEffect(() => {
    const t = setInterval(() => tick((n) => n + 1), 30_000)
    return () => clearInterval(t)
  }, [])
  if (!date) return <span className={className}>—</span>
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <time dateTime={date} className={className} suppressHydrationWarning>
          {formatRelative(date)}
        </time>
      </TooltipTrigger>
      <TooltipContent>{formatDateTime(date)}</TooltipContent>
    </Tooltip>
  )
}
