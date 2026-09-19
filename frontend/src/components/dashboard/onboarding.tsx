"use client"

import { Check, ChevronRight } from "lucide-react"
import Link from "next/link"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { cn } from "@/lib/utils"

interface Step {
  title: string
  description: string
  href: string
  done: boolean
}

/** First-run checklist, derived from real state. Hidden once complete. */
export function Onboarding({ databases, storage, schedules, backups }: { databases: number; storage: number; schedules: number; backups: number }) {
  const steps: Step[] = [
    { title: "Connect a PostgreSQL database", description: "Host, credentials and a connection test.", href: "/databases/new", done: databases > 0 },
    { title: "Add a storage destination", description: "S3, Cloudflare R2, MinIO or local disk.", href: "/storage?new=1", done: storage > 0 },
    { title: "Create a backup schedule", description: "Frequency, retention, compression and encryption.", href: "/schedules?new=1", done: schedules > 0 },
    { title: "Run your first backup", description: "Watch pg_dump, encryption and upload live.", href: "/backups", done: backups > 0 },
  ]
  const done = steps.filter((s) => s.done).length
  if (done === steps.length) return null
  const next = steps.findIndex((s) => !s.done)
  return (
    <Card className="gap-0 overflow-hidden py-0">
      <CardHeader className="border-b py-4">
        <div className="flex items-center justify-between gap-4">
          <div>
            <CardTitle>Get protected in a few minutes</CardTitle>
            <CardDescription>
              {done} of {steps.length} steps complete
            </CardDescription>
          </div>
          <Progress value={(done / steps.length) * 100} className="h-1.5 w-32" />
        </div>
      </CardHeader>
      <CardContent className="grid p-0 sm:grid-cols-2 lg:grid-cols-4">
        {steps.map((s, i) => (
          <Link
            key={s.title}
            href={s.href}
            className={cn(
              "group flex gap-3 border-b p-4 transition-colors hover:bg-muted/50 sm:[&:nth-child(odd)]:border-r lg:border-b-0 lg:border-r lg:last:border-r-0",
              i === next && "bg-brand/5",
            )}
          >
            <span
              className={cn(
                "mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full border text-[11px] font-medium",
                s.done ? "border-success bg-success text-background" : i === next ? "border-brand text-brand" : "text-muted-foreground",
              )}
            >
              {s.done ? <Check className="size-3" /> : i + 1}
            </span>
            <span className="min-w-0 flex-1">
              <span className={cn("block text-sm font-medium", s.done && "text-muted-foreground line-through decoration-muted-foreground/40")}>{s.title}</span>
              <span className="block text-xs text-muted-foreground">{s.description}</span>
            </span>
            {!s.done && <ChevronRight className="mt-0.5 size-4 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />}
          </Link>
        ))}
      </CardContent>
    </Card>
  )
}
