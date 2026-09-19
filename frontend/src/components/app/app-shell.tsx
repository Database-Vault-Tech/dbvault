"use client"

import { AlertTriangle, Menu } from "lucide-react"
import { useState, type ReactNode } from "react"

import { LogoMark } from "@/components/brand/logo"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { OrgProvider } from "@/lib/org"
import { useMe, useSystemStatus } from "@/lib/queries"

import { ActivityBell } from "./activity-bell"
import { ThemeToggle } from "./theme-toggle"
import { CommandMenu } from "./command-menu"
import { ErrorState } from "./error-state"
import { OrgSwitcher } from "./org-switcher"
import { Sidebar, SidebarContent } from "./sidebar"
import { UserMenu } from "./user-menu"

function WorkerBanner() {
  const { data } = useSystemStatus()
  if (!data || data.workers_online > 0) return null
  return (
    <div className="flex items-start gap-2 border-b border-warning/30 bg-warning/10 px-4 py-2.5 text-sm sm:px-6 lg:px-8">
      <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />
      <p>
        <span className="font-medium">No worker is running.</span>{" "}
        <span className="text-muted-foreground">
          Backups, restores and verifications stay queued until a worker starts (<code className="font-mono text-xs">docker compose up -d worker</code>).
        </span>
      </p>
    </div>
  )
}

function ShellSkeleton() {
  return (
    <div className="flex min-h-screen">
      <div className="hidden w-60 border-r bg-sidebar p-4 lg:block">
        <Skeleton className="h-6 w-28" />
        <div className="mt-8 space-y-2">
          {Array.from({ length: 7 }).map((_, i) => (
            <Skeleton key={i} className="h-7 w-full" />
          ))}
        </div>
      </div>
      <div className="flex-1 p-8">
        <Skeleton className="h-8 w-48" />
        <div className="mt-8 grid gap-4 md:grid-cols-3">
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
        </div>
      </div>
    </div>
  )
}

export function AppShell({ children }: { children: ReactNode }) {
  const { data: me, error, refetch, isPending } = useMe()
  const [mobileOpen, setMobileOpen] = useState(false)

  if (isPending) return <ShellSkeleton />
  if (error || !me) {
    return (
      <div className="mx-auto max-w-lg p-8">
        <ErrorState error={error} retry={() => refetch()} title="Couldn't load your account" />
      </div>
    )
  }

  return (
    <OrgProvider me={me}>
      <Sidebar />
      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent side="left" className="w-64 bg-sidebar p-0">
          <SheetTitle className="sr-only">Navigation</SheetTitle>
          <SidebarContent onNavigate={() => setMobileOpen(false)} />
        </SheetContent>
      </Sheet>
      <div className="flex min-h-screen flex-col lg:pl-60">
        <header className="sticky top-0 z-20 flex h-14 items-center gap-2 border-b bg-background/85 px-3 backdrop-blur supports-[backdrop-filter]:bg-background/70 sm:px-6">
          <Button variant="ghost" size="icon" className="lg:hidden" aria-label="Open navigation" onClick={() => setMobileOpen(true)}>
            <Menu />
          </Button>
          <LogoMark className="size-6 lg:hidden" />
          <OrgSwitcher />
          <div className="ml-auto flex flex-1 items-center justify-end gap-1.5 sm:gap-2">
            <div className="hidden flex-1 justify-end md:flex">
              <CommandMenu />
            </div>
            <ThemeToggle />
            <ActivityBell />
            <UserMenu />
          </div>
        </header>
        <WorkerBanner />
        <main className="mx-auto w-full max-w-7xl flex-1 px-4 py-6 sm:px-6 lg:px-8 lg:py-8">{children}</main>
      </div>
    </OrgProvider>
  )
}
