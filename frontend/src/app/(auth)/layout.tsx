import type { Metadata } from "next"
import Link from "next/link"

import { ThemeToggle } from "@/components/app/theme-toggle"
import { Logo } from "@/components/brand/logo"

// Sign-in pages have no search value and must not be indexed.
export const metadata: Metadata = { robots: { index: false, follow: false } }

const LOG = [
  ["12:30:01", "Connecting to PostgreSQL"],
  ["12:30:03", "Connection successful (PostgreSQL 17.2)"],
  ["12:30:04", "Starting pg_dump"],
  ["12:31:52", "Compression completed with zstd (7.4x)"],
  ["12:31:54", "Encryption completed (age X25519)"],
  ["12:32:17", "Upload completed (482 MB)"],
  ["12:32:18", "Checksum verified (sha256:9f2c4e1a…)"],
  ["12:32:18", "Backup completed"],
]

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid min-h-screen lg:grid-cols-[1fr_minmax(0,560px)] xl:grid-cols-[1fr_640px]">
      <div className="flex flex-col px-6 py-8 sm:px-10">
        <div className="flex items-center justify-between">
          <Link href="/" className="w-fit">
            <Logo />
          </Link>
          <ThemeToggle />
        </div>
        <div className="flex flex-1 items-center justify-center py-12">
          <div className="w-full max-w-sm">{children}</div>
        </div>
        <p className="text-center text-xs text-muted-foreground">Open-source SQL database backups. Your database, your backups, your infrastructure.</p>
      </div>
      <aside className="relative hidden overflow-hidden border-l bg-muted/40 text-foreground lg:flex lg:flex-col lg:justify-between lg:p-12">
        <div className="absolute inset-0 bg-grid mask-radial opacity-60" aria-hidden />
        <div className="relative space-y-4">
          <p className="text-sm font-medium text-brand">Restore-tested backups</p>
          <h2 className="max-w-md text-3xl font-semibold tracking-tight text-balance">A backup you haven&apos;t restored is just a hope.</h2>
          <p className="max-w-md text-muted-foreground">
            DBVault encrypts every dump, verifies its checksum, and can prove it restores in a disposable sandbox database.
          </p>
        </div>
        <div className="dark relative rounded-xl border bg-card text-card-foreground shadow-2xl">
          <div className="flex items-center gap-1.5 border-b px-4 py-2.5">
            <span className="size-2.5 rounded-full bg-muted-foreground/30" />
            <span className="size-2.5 rounded-full bg-muted-foreground/30" />
            <span className="size-2.5 rounded-full bg-muted-foreground/30" />
            <span className="ml-2 font-mono text-xs text-muted-foreground">backup · production</span>
          </div>
          <ol className="space-y-1 p-4 font-mono text-[12.5px]">
            {LOG.map(([t, m], i) => (
              <li key={i} className="flex gap-3 animate-fade-up" style={{ animationDelay: `${i * 90}ms` }}>
                <span className="text-muted-foreground/70">{t}</span>
                <span className={i === LOG.length - 1 ? "text-success" : ""}>{m}</span>
              </li>
            ))}
          </ol>
        </div>
      </aside>
    </div>
  )
}
