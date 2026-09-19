"use client"

import { useEffect, useRef } from "react"

import { formatTime } from "@/lib/format"
import type { LogEntry } from "@/lib/types"
import { cn } from "@/lib/utils"

// The log panel is always dark, so levels use fixed light-on-dark colors.
const LEVEL_CLASS: Record<string, string> = {
  info: "text-zinc-100",
  warn: "text-amber-300",
  error: "text-red-400",
  debug: "text-zinc-500",
}

/** Terminal-style job log. Auto-scrolls while new lines arrive. */
export function LogViewer({
  logs,
  live = false,
  className,
  emptyText = "No log output yet.",
}: {
  logs: LogEntry[] | undefined
  live?: boolean
  className?: string
  emptyText?: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (live && ref.current) ref.current.scrollTop = ref.current.scrollHeight
  }, [logs?.length, live])
  return (
    <div
      ref={ref}
      className={cn("max-h-96 overflow-auto rounded-lg border bg-zinc-950 p-3 font-mono text-[12.5px] leading-6 text-zinc-100", className)}
      role="log"
      aria-live={live ? "polite" : "off"}
    >
      {!logs?.length ? (
        <p className="text-zinc-500">{emptyText}</p>
      ) : (
        <ol>
          {logs.map((l) => (
            <li key={l.id} className="flex gap-3 whitespace-pre-wrap">
              <span className="shrink-0 text-zinc-500 tabular select-none">{formatTime(l.created_at)}</span>
              <span className={cn("min-w-0 break-words", LEVEL_CLASS[l.level])}>
                {l.message}
              </span>
            </li>
          ))}
          {live && (
            <li className="flex gap-3">
              <span className="shrink-0 text-zinc-500 select-none">{"        "}</span>
              <span className="inline-block h-4 w-2 translate-y-1 animate-pulse bg-zinc-400" aria-hidden />
            </li>
          )}
        </ol>
      )}
    </div>
  )
}
