"use client"

import { Bell } from "lucide-react"
import Link from "next/link"

import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { humanizeAction } from "@/lib/format"
import { useAuditLogs } from "@/lib/queries"

import { RelativeTime } from "./relative-time"
import { StatusDot, type Tone } from "./status"

const WATCHED = ["backup.", "restore.", "storage."]

function toneForAction(action: string): Tone {
  if (action.endsWith("failed") || action.endsWith("verification_failed")) return "error"
  if (action.endsWith("completed") || action.endsWith("verified")) return "success"
  if (action.endsWith("started") || action.endsWith("requested")) return "running"
  return "neutral"
}

/** Recent backup/restore activity, highlighting failures. */
export function ActivityBell() {
  const { data } = useAuditLogs({ limit: 40 })
  const events = (data?.pages[0]?.data ?? []).filter((l) => WATCHED.some((w) => l.action.startsWith(w))).slice(0, 8)
  const failures = events.filter((e) => toneForAction(e.action) === "error").length
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" className="relative" aria-label="Recent activity">
          <Bell />
          {failures > 0 && <span className="absolute top-1.5 right-1.5 size-2 rounded-full bg-destructive ring-2 ring-background" />}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 p-0">
        <div className="flex items-center justify-between border-b px-3 py-2.5">
          <span className="text-sm font-medium">Recent activity</span>
          <Link href="/audit-logs" className="text-xs text-muted-foreground hover:text-foreground">
            View all
          </Link>
        </div>
        {events.length === 0 ? (
          <p className="px-3 py-6 text-center text-sm text-muted-foreground">No backup activity yet.</p>
        ) : (
          <ul className="max-h-80 divide-y overflow-auto">
            {events.map((e) => {
              const href = e.resource_type === "backup" && e.resource_id ? `/backups/${e.resource_id}` : e.resource_type === "restore" ? "/restore" : "/audit-logs"
              const name = (e.metadata?.database as string) ?? (e.metadata?.target as string) ?? ""
              return (
                <li key={e.id}>
                  <Link href={href} className="flex gap-2.5 px-3 py-2.5 hover:bg-muted/60">
                    <StatusDot tone={toneForAction(e.action)} className="mt-1.5" />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm">
                        {humanizeAction(e.action)}
                        {name && <span className="text-muted-foreground"> · {name}</span>}
                      </div>
                      <RelativeTime date={e.created_at} className="text-xs text-muted-foreground" />
                    </div>
                  </Link>
                </li>
              )
            })}
          </ul>
        )}
      </PopoverContent>
    </Popover>
  )
}
