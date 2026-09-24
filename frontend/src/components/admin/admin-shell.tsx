"use client"

import { Eye, ShieldHalf } from "lucide-react"
import Link from "next/link"
import { usePathname } from "next/navigation"
import type { ReactNode } from "react"

import { EmptyState } from "@/components/app/empty-state"
import { PageHeader } from "@/components/app/page-header"
import { useOrg } from "@/lib/org"
import { cn } from "@/lib/utils"

const ITEMS = [
  { href: "/admin", label: "Organizations" },
  { href: "/admin/users", label: "Users" },
]

/** Header, tabs and access check shared by every instance-admin page. */
export function AdminShell({ children, header }: { children: ReactNode; header?: ReactNode }) {
  const { me } = useOrg()
  const pathname = usePathname()
  if (!me.is_instance_admin) {
    return <EmptyState icon={ShieldHalf} title="Page not found" description="This page doesn't exist or you don't have access to it." />
  }
  return (
    <div>
      {header ?? (
        <PageHeader
          title="Instance admin"
          description="Every organization on this DBVault installation. Read-only: credentials, storage settings and backup contents are never shown."
          actions={
            <span className="inline-flex items-center gap-1.5 rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground">
              <Eye className="size-3.5" /> Read-only
            </span>
          }
        />
      )}
      {!header && (
        <nav className="-mt-2 mb-6 flex gap-1 border-b" aria-label="Instance admin">
          {ITEMS.map((i) => {
            const active = i.href === "/admin" ? pathname === "/admin" : pathname.startsWith(i.href)
            return (
              <Link
                key={i.href}
                href={i.href}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "-mb-px border-b-2 border-transparent px-3 py-2 text-sm text-muted-foreground transition-colors hover:text-foreground",
                  active && "border-foreground font-medium text-foreground",
                )}
              >
                {i.label}
              </Link>
            )
          })}
        </nav>
      )}
      {children}
    </div>
  )
}
