"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"

import { cn } from "@/lib/utils"

const ITEMS = [
  { href: "/settings", label: "General" },
  { href: "/settings/team", label: "Team" },
  { href: "/settings/security", label: "Security" },
]

export function SettingsNav() {
  const pathname = usePathname()
  return (
    <nav className="-mt-2 mb-6 flex gap-1 border-b" aria-label="Settings">
      {ITEMS.map((i) => (
        <Link
          key={i.href}
          href={i.href}
          aria-current={pathname === i.href ? "page" : undefined}
          className={cn(
            "-mb-px border-b-2 border-transparent px-3 py-2 text-sm text-muted-foreground transition-colors hover:text-foreground",
            pathname === i.href && "border-foreground font-medium text-foreground",
          )}
        >
          {i.label}
        </Link>
      ))}
    </nav>
  )
}
