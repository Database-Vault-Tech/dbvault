import {
  AlertOctagon,
  Archive,
  Bell,
  CalendarClock,
  Database,
  HardDrive,
  LayoutDashboard,
  Play,
  RotateCcw,
  Search,
  Settings,
  ShieldCheck,
  Users,
} from "lucide-react"

import { LogoMark } from "@/components/brand/logo"
import { cn } from "@/lib/utils"

const NAV = [
  { label: "Dashboard", icon: LayoutDashboard, active: true },
  { label: "Databases", icon: Database },
  { label: "Backups", icon: Archive },
  { label: "Restore", icon: RotateCcw },
  { label: "Storage", icon: HardDrive },
  { label: "Schedules", icon: CalendarClock },
  { label: "Notifications", icon: Bell },
  { label: "Team", icon: Users },
  { label: "Settings", icon: Settings },
]

const STATS = [
  { label: "Databases", value: "12", icon: Database, hint: "Connected" },
  { label: "Protected", value: "11", icon: ShieldCheck, hint: "1 without a schedule" },
  { label: "Backups today", value: "42", icon: Archive, hint: "2 running now" },
  { label: "Storage", value: "184 GB", icon: HardDrive, hint: "Across 3 destinations" },
]

type Row = { db: string; status: "Completed" | "Running" | "Verified" | "Failed"; size: string; duration: string; storage: string; created: string }

const ROWS: Row[] = [
  { db: "production", status: "Completed", size: "482 MB", duration: "2m 14s", storage: "S3", created: "12 minutes ago" },
  { db: "staging", status: "Completed", size: "312 MB", duration: "1m 03s", storage: "R2", created: "1 hour ago" },
  { db: "analytics", status: "Running", size: "1.2 GB", duration: "—", storage: "S3", created: "just now" },
  { db: "billing", status: "Verified", size: "96 MB", duration: "38s", storage: "MinIO", created: "3 hours ago" },
  { db: "auth", status: "Completed", size: "54 MB", duration: "21s", storage: "S3", created: "6 hours ago" },
]

const STATUS_CLASS: Record<Row["status"], { pill: string; dot: string }> = {
  Completed: { pill: "bg-success/15 text-success", dot: "bg-success" },
  Verified: { pill: "bg-success/15 text-success", dot: "bg-success" },
  Running: { pill: "bg-info/15 text-info", dot: "bg-info animate-pulse-dot" },
  Failed: { pill: "bg-destructive/15 text-destructive", dot: "bg-destructive" },
}

const BARS = [6, 7, 7, 8, 6, 9, 8, 7, 10, 9, 8, 11, 9, 12]

/** A faithful, HTML-rendered preview of the DBVault dashboard. */
export function DashboardPreview() {
  return (
    <figure aria-label="Preview of the DBVault dashboard" className="relative">
      <div className="pointer-events-none absolute -inset-x-8 -top-8 bottom-0 rounded-[2rem] bg-brand/5 blur-3xl" aria-hidden />
      <div className="relative overflow-hidden rounded-xl border bg-card shadow-2xl shadow-black/40 ring-1 ring-white/5">
        {/* Browser chrome */}
        <div className="flex items-center gap-3 border-b bg-background/60 px-4 py-2.5">
          <div className="flex gap-1.5" aria-hidden>
            <span className="size-2.5 rounded-full bg-muted-foreground/25" />
            <span className="size-2.5 rounded-full bg-muted-foreground/25" />
            <span className="size-2.5 rounded-full bg-muted-foreground/25" />
          </div>
          <div className="mx-auto flex max-w-xs flex-1 items-center justify-center rounded-md border bg-card px-3 py-1 font-mono text-[11px] text-muted-foreground">
            localhost:3000/dashboard
          </div>
          <div className="w-12" aria-hidden />
        </div>

        <div className="flex text-left" aria-hidden>
          {/* Sidebar */}
          <div className="hidden w-48 shrink-0 flex-col gap-5 border-r bg-sidebar px-3 py-4 lg:flex">
            <div className="flex items-center gap-2 px-2 text-sm font-semibold">
              <LogoMark className="size-5" /> DBVault
            </div>
            <div className="flex flex-col gap-0.5">
              {NAV.map((n) => (
                <div
                  key={n.label}
                  className={cn(
                    "flex h-7 items-center gap-2 rounded-md px-2 text-[12.5px] text-muted-foreground",
                    n.active && "bg-sidebar-accent font-medium text-foreground",
                  )}
                >
                  <n.icon className="size-3.5" />
                  {n.label}
                </div>
              ))}
            </div>
            <div className="mt-auto space-y-1 rounded-lg border bg-background/40 p-2.5 text-[11px]">
              <div className="flex items-center gap-1.5">
                <span className="size-1.5 rounded-full bg-success" /> 2 workers online
              </div>
              <div className="flex items-center gap-1.5 text-muted-foreground">
                <span className="size-1.5 rounded-full bg-success" /> Restore testing (docker)
              </div>
            </div>
          </div>

          {/* Main */}
          <div className="min-w-0 flex-1">
            <div className="flex h-11 items-center gap-2 border-b px-4">
              <span className="flex size-4 items-center justify-center rounded bg-foreground text-[8px] font-semibold text-background">AC</span>
              <span className="text-[12.5px] font-medium">Acme Inc.</span>
              <div className="ml-auto hidden items-center gap-2 rounded-md border px-2 py-1 text-[11px] text-muted-foreground sm:flex">
                <Search className="size-3" /> Search… <span className="rounded border px-1 font-mono text-[10px]">⌘K</span>
              </div>
            </div>
            <div className="space-y-5 p-4 sm:p-6">
              <div className="flex items-end justify-between gap-4">
                <div>
                  <div className="text-[11px] text-muted-foreground">Welcome back, Ada</div>
                  <div className="text-lg font-semibold tracking-tight">Overview</div>
                </div>
                <div className="flex h-7 items-center gap-1.5 rounded-md bg-primary px-2.5 text-[12px] font-medium text-primary-foreground">
                  <Play className="size-3" /> Run backup
                </div>
              </div>

              <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
                {STATS.map((s) => (
                  <div key={s.label} className="rounded-lg border bg-background/40 p-3">
                    <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                      {s.label}
                      <s.icon className="size-3.5" />
                    </div>
                    <div className="mt-1.5 text-xl font-semibold tracking-tight tabular sm:text-2xl">{s.value}</div>
                    <div className="mt-0.5 truncate text-[10.5px] text-muted-foreground">{s.hint}</div>
                  </div>
                ))}
              </div>

              <div className="grid gap-4 xl:grid-cols-[1fr_220px]">
                <div className="min-w-0 overflow-hidden rounded-lg border">
                  <div className="flex items-center justify-between border-b bg-background/40 px-3 py-2">
                    <span className="text-[12px] font-semibold">Recent backups</span>
                    <span className="text-[11px] text-muted-foreground">View all</span>
                  </div>
                  <table className="w-full text-[12px]">
                    <thead className="text-muted-foreground">
                      <tr className="border-b">
                        <th className="px-3 py-2 text-left font-medium">Database</th>
                        <th className="px-3 py-2 text-left font-medium">Status</th>
                        <th className="px-3 py-2 text-right font-medium">Size</th>
                        <th className="hidden px-3 py-2 text-right font-medium sm:table-cell">Duration</th>
                        <th className="hidden px-3 py-2 text-left font-medium md:table-cell">Storage</th>
                        <th className="hidden px-3 py-2 text-right font-medium md:table-cell">Created</th>
                      </tr>
                    </thead>
                    <tbody>
                      {ROWS.map((r) => (
                        <tr key={r.db} className="border-b last:border-0">
                          <td className="px-3 py-2.5 font-medium">{r.db}</td>
                          <td className="px-3 py-2.5">
                            <span className={cn("inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium", STATUS_CLASS[r.status].pill)}>
                              {r.status === "Running" && <span className={cn("size-1.5 rounded-full", STATUS_CLASS[r.status].dot)} />}
                              {r.status}
                            </span>
                          </td>
                          <td className="px-3 py-2.5 text-right tabular">{r.size}</td>
                          <td className="hidden px-3 py-2.5 text-right tabular sm:table-cell">{r.duration}</td>
                          <td className="hidden px-3 py-2.5 md:table-cell">{r.storage}</td>
                          <td className="hidden px-3 py-2.5 text-right text-muted-foreground md:table-cell">{r.created}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <div className="hidden rounded-lg border p-3 xl:block">
                  <div className="text-[12px] font-semibold">Last 14 days</div>
                  <div className="text-[11px] text-muted-foreground">Backups per day</div>
                  <div className="mt-4 flex h-28 items-end gap-1">
                    {BARS.map((b, i) => (
                      <div key={i} className="flex h-full flex-1 flex-col justify-end gap-px">
                        {i === 5 && <div className="h-[8%] rounded-sm bg-destructive/80" />}
                        <div className="rounded-sm bg-brand/70" style={{ height: `${(b / 12) * 100}%` }} />
                      </div>
                    ))}
                  </div>
                  <div className="mt-3 flex items-center gap-1.5 text-[11px] text-muted-foreground">
                    <AlertOctagon className="size-3 text-destructive" /> 1 failed · retried
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
      <div className="pointer-events-none absolute inset-x-0 bottom-0 h-24 bg-gradient-to-t from-background to-transparent" aria-hidden />
    </figure>
  )
}
