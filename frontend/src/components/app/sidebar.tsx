"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"

import { Logo } from "@/components/brand/logo"
import { useSystemStatus } from "@/lib/queries"
import { formatVersion } from "@/lib/format"
import { cn } from "@/lib/utils"

import { isActive, primaryNav, secondaryNav, type NavItem } from "./nav"
import { StatusDot } from "./status"

function NavLink({ item, onNavigate }: { item: NavItem; onNavigate?: () => void }) {
  const pathname = usePathname()
  const active = isActive(pathname, item)
  return (
    <Link
      href={item.href}
      onClick={onNavigate}
      aria-current={active ? "page" : undefined}
      className={cn(
        "group flex h-8 items-center gap-2.5 rounded-md px-2.5 text-sm text-sidebar-foreground/70 transition-colors",
        "hover:bg-sidebar-accent hover:text-sidebar-foreground",
        active && "bg-sidebar-accent font-medium text-sidebar-foreground",
      )}
    >
      <item.icon className={cn("size-4 text-sidebar-foreground/55 group-hover:text-sidebar-foreground", active && "text-sidebar-foreground")} />
      {item.label}
    </Link>
  )
}

function SystemHealth() {
  const { data } = useSystemStatus()
  if (!data) return null
  const online = data.workers_online > 0
  return (
    <div className="space-y-1.5 rounded-lg border border-sidebar-border bg-background/40 p-3 text-xs">
      <div className="flex items-center gap-2">
        <StatusDot tone={online ? "success" : "error"} />
        <span className="font-medium">{online ? `${data.workers_online} worker${data.workers_online > 1 ? "s" : ""} online` : "No worker online"}</span>
      </div>
      <div className="flex items-center gap-2 text-muted-foreground">
        <StatusDot tone={data.verification.available ? "success" : "warning"} />
        <span>Restore testing {data.verification.available ? `(${data.verification.mode})` : "unavailable"}</span>
      </div>
      <div className="truncate pt-1 font-mono text-[10.5px] text-muted-foreground/70" title={data.version}>
        DBVault {formatVersion(data.version)}
      </div>
    </div>
  )
}

export function SidebarContent({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <div className="flex h-full flex-col gap-6 px-3 py-4">
      <Link href="/dashboard" className="px-2" onClick={onNavigate}>
        <Logo />
      </Link>
      <nav className="flex flex-col gap-0.5" aria-label="Main">
        {primaryNav.map((item) => (
          <NavLink key={item.href} item={item} onNavigate={onNavigate} />
        ))}
      </nav>
      <nav className="flex flex-col gap-0.5" aria-label="Organization">
        <div className="px-2.5 pb-1 text-[11px] font-medium tracking-wide text-muted-foreground/80 uppercase">Organization</div>
        {secondaryNav.map((item) => (
          <NavLink key={item.href} item={item} onNavigate={onNavigate} />
        ))}
      </nav>
      <div className="mt-auto">
        <SystemHealth />
      </div>
    </div>
  )
}

export function Sidebar() {
  return (
    <aside className="fixed inset-y-0 left-0 z-30 hidden w-60 border-r border-sidebar-border bg-sidebar lg:block">
      <SidebarContent />
    </aside>
  )
}
