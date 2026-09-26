"use client"

import { Menu } from "lucide-react"
import Link from "next/link"
import { usePathname } from "next/navigation"
import { useState } from "react"

import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet"
import { DOC_GROUPS, DOCS } from "@/lib/docs-nav"
import { cn } from "@/lib/utils"

function NavList({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname()
  return (
    <nav aria-label="Documentation" className="space-y-6">
      <Link
        href="/docs"
        onClick={onNavigate}
        aria-current={pathname === "/docs" ? "page" : undefined}
        className={cn(
          "block rounded-md px-2 py-1.5 text-sm transition-colors",
          pathname === "/docs" ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground",
        )}
      >
        Overview
      </Link>
      {DOC_GROUPS.map((group) => (
        <div key={group}>
          <div className="mb-1.5 px-2 text-xs font-medium tracking-wide text-muted-foreground/80 uppercase">{group}</div>
          <ul className="space-y-0.5">
            {DOCS.filter((d) => d.group === group).map((d) => {
              const active = pathname === `/docs/${d.slug}`
              return (
                <li key={d.slug}>
                  <Link
                    href={`/docs/${d.slug}`}
                    onClick={onNavigate}
                    aria-current={active ? "page" : undefined}
                    className={cn(
                      "block rounded-md px-2 py-1.5 text-sm transition-colors",
                      active ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {d.title}
                  </Link>
                </li>
              )
            })}
          </ul>
        </div>
      ))}
    </nav>
  )
}

/** Sidebar on large screens. */
export function DocsSidebar() {
  return (
    <aside className="sticky top-16 hidden h-[calc(100vh-4rem)] w-60 shrink-0 overflow-y-auto border-r py-8 pr-4 lg:block">
      <NavList />
    </aside>
  )
}

/** Menu button that opens the same navigation on small screens. */
export function DocsMobileNav() {
  const [open, setOpen] = useState(false)
  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button variant="ghost" size="icon" className="lg:hidden" aria-label="Open documentation menu">
          <Menu />
        </Button>
      </SheetTrigger>
      <SheetContent side="left" className="w-72 overflow-y-auto bg-background p-4 text-foreground">
        <SheetTitle className="mb-4 text-sm">Documentation</SheetTitle>
        <NavList onNavigate={() => setOpen(false)} />
      </SheetContent>
    </Sheet>
  )
}
