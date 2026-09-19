"use client"

import type { ReactNode } from "react"

export function AuthHeader({ title, description }: { title: string; description: ReactNode }) {
  return (
    <div className="mb-8 space-y-2">
      <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
      <p className="text-sm text-muted-foreground">{description}</p>
    </div>
  )
}

/** Only allow same-site relative redirects after login (no open redirects). */
export function safeNext(next: string | null | undefined, fallback = "/dashboard"): string {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.startsWith("/\\")) return fallback
  return next
}
