import {
  Archive,
  BadgeCheck,
  Bell,
  CalendarClock,
  Cloud,
  Container,
  Database,
  History,
  Lock,
  RotateCcw,
  Scale,
  Terminal as TerminalIcon,
} from "lucide-react"

import { Section, SectionHeading } from "./primitives"

const FEATURES = [
  {
    icon: Archive,
    title: "Automated backups",
    body: "A Go worker runs pg_dump in custom format and streams it through compression, encryption and upload in a single pass — nothing is staged on disk.",
  },
  {
    icon: CalendarClock,
    title: "Scheduled backups",
    body: "Hourly, every 6 hours, daily, weekly or any cron expression, evaluated server-side in the timezone you choose. No browser tab required.",
  },
  {
    icon: Database,
    title: "PostgreSQL support",
    body: "Uses the official pg_dump and pg_restore tooling, checks client/server version compatibility, and supports every libpq SSL mode.",
  },
  {
    icon: Cloud,
    title: "S3 / R2 / MinIO",
    body: "Any S3-compatible bucket via streaming multipart uploads, plus local disk. Failed uploads are aborted so no partial objects are left behind.",
  },
  {
    icon: Lock,
    title: "Encryption",
    body: "Backups are encrypted with age (X25519 + ChaCha20-Poly1305) using a per-organization key sealed by an AES-256-GCM master key.",
  },
  {
    icon: BadgeCheck,
    title: "Backup verification",
    body: "SHA-256 checksums are verified after every upload, and restore tests run in a disposable PostgreSQL container or sandbox server.",
  },
  {
    icon: RotateCcw,
    title: "Restore",
    body: "Restore into an existing database in a single all-or-nothing transaction, or into a brand-new database. Destructive restores require typing RESTORE.",
  },
  {
    icon: History,
    title: "Retention policies",
    body: "Grandfather-father-son retention — keep N daily, weekly and monthly backups — with a preview of exactly what will be kept or deleted.",
  },
  {
    icon: Bell,
    title: "Notifications",
    body: "Email and HMAC-signed webhooks for failed backups, verification results, restores and storage failures, with delivery history and retries.",
  },
  {
    icon: TerminalIcon,
    title: "CLI",
    body: "dbvault backup production from your terminal or CI, with live progress, API tokens, and commands for databases, storage and schedules.",
  },
  {
    icon: Container,
    title: "Docker",
    body: "One docker compose up -d starts the API, worker, scheduler, dashboard, PostgreSQL, Redis and MinIO — secrets are generated on first boot.",
  },
  {
    icon: Scale,
    title: "Open source",
    body: "Apache-2.0 licensed, with organizations, owner/admin/member/viewer roles and a full audit log. Read every line that touches your data.",
  },
]

export function Features() {
  return (
    <Section id="features" labelledBy="features-title">
      <SectionHeading
        id="features-title"
        eyebrow="Features"
        title="Everything a backup system needs. Nothing it doesn't."
        description="DBVault covers the full lifecycle — dump, protect, store, prove, restore — with sensible defaults and no custom cryptography."
      />
      <div className="mt-14 grid gap-px overflow-hidden rounded-xl border bg-border sm:grid-cols-2 lg:grid-cols-3">
        {FEATURES.map((f) => (
          <article key={f.title} className="group bg-background p-6 transition-colors hover:bg-card">
            <div className="flex size-9 items-center justify-center rounded-lg border bg-card text-brand transition-colors group-hover:border-brand/40">
              <f.icon className="size-4" aria-hidden />
            </div>
            <h3 className="mt-4 font-medium">{f.title}</h3>
            <p className="mt-1.5 text-sm leading-relaxed text-muted-foreground">{f.body}</p>
          </article>
        ))}
      </div>
    </Section>
  )
}
