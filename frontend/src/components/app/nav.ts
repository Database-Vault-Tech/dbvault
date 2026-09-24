import {
  Archive,
  Bell,
  CalendarClock,
  Database,
  HardDrive,
  LayoutDashboard,
  RotateCcw,
  ScrollText,
  Settings,
  ShieldHalf,
  Users,
  type LucideIcon,
} from "lucide-react"

export interface NavItem {
  label: string
  href: string
  icon: LucideIcon
  /** Paths that should also highlight this item. */
  match?: string[]
}

export const primaryNav: NavItem[] = [
  { label: "Dashboard", href: "/dashboard", icon: LayoutDashboard },
  { label: "Databases", href: "/databases", icon: Database },
  { label: "Backups", href: "/backups", icon: Archive },
  { label: "Restore", href: "/restore", icon: RotateCcw },
  { label: "Storage", href: "/storage", icon: HardDrive },
  { label: "Schedules", href: "/schedules", icon: CalendarClock },
  { label: "Notifications", href: "/notifications", icon: Bell },
]

export const secondaryNav: NavItem[] = [
  { label: "Team", href: "/settings/team", icon: Users },
  { label: "Audit log", href: "/audit-logs", icon: ScrollText },
  { label: "Settings", href: "/settings", icon: Settings, match: ["/settings/security"] },
]

export const adminNav: NavItem = { label: "Instance admin", href: "/admin", icon: ShieldHalf }

export function isActive(pathname: string, item: NavItem): boolean {
  if (item.href === "/settings") return pathname === "/settings" || (item.match ?? []).some((m) => pathname.startsWith(m))
  return pathname === item.href || pathname.startsWith(item.href + "/")
}
