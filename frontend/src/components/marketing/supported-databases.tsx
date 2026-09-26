import { Database } from "lucide-react"

import { cn } from "@/lib/utils"

import { Section, SectionHeading } from "./primitives"

// Keep this honest: only mark an engine "available" once DBVault can back it
// up and restore it end to end.
const ENGINES = [
  { name: "PostgreSQL", tool: "pg_dump · pg_restore", detail: "Versions 9.2 – 18, every SSL mode, restore-tested in a sandbox.", available: true },
  {
    name: "MySQL",
    tool: "mariadb-dump · mariadb",
    detail: "MySQL 5.7 – 9, consistent InnoDB snapshots with routines, triggers and events, restore-tested in a sandbox.",
    available: true,
  },
  { name: "MariaDB", tool: "mariadb-dump · mariadb", detail: "MariaDB 10 and 11, native dump and restore, restore-tested in a sandbox.", available: true },
  {
    name: "SQLite",
    tool: "VACUUM INTO · atomic swap",
    detail: "Consistent snapshots of live SQLite files from a mounted folder, restore-tested on every verification.",
    available: true,
  },
  { name: "SQL Server", tool: "sqlpackage / BACKUP", detail: "Native backups for Microsoft SQL Server.", available: false },
]

export function SupportedDatabases() {
  return (
    <Section id="databases" labelledBy="databases-title">
      <div className="grid gap-12 lg:grid-cols-[1fr_1.4fr] lg:items-start">
        <SectionHeading
          id="databases-title"
          eyebrow="Supported databases"
          title="One vault for every SQL database."
          description="DBVault drives each engine's own battle-tested dump tooling through the same pipeline: compression, encryption, checksums, storage, retention and restore tests. PostgreSQL, MySQL, MariaDB and SQLite are fully supported today; SQL Server is on the way."
        />
        <ul className="grid gap-3 sm:grid-cols-2">
          {ENGINES.map((e) => (
            <li key={e.name} className={cn("rounded-xl border p-5", e.available ? "border-brand/40 bg-brand/5" : "bg-card/50")}>
              <div className="flex items-center justify-between gap-3">
                <span className="flex items-center gap-2.5">
                  <Database className={cn("size-4", e.available ? "text-brand" : "text-muted-foreground")} aria-hidden />
                  <span className="font-medium tracking-tight">{e.name}</span>
                </span>
                <span
                  className={cn(
                    "rounded-full px-2 py-0.5 text-xs font-medium",
                    e.available ? "bg-brand/15 text-brand" : "bg-muted text-muted-foreground",
                  )}
                >
                  {e.available ? "Available" : "Coming soon"}
                </span>
              </div>
              <p className="mt-2 font-mono text-xs text-muted-foreground">{e.tool}</p>
              <p className="mt-1.5 text-sm leading-relaxed text-muted-foreground">{e.detail}</p>
            </li>
          ))}
        </ul>
      </div>
    </Section>
  )
}
