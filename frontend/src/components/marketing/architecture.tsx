import { ArrowDown, ArrowRight, Cpu } from "lucide-react"
import { Fragment } from "react"

import { Section, SectionHeading } from "./primitives"

const STAGES = [
  { name: "Your database", detail: "PostgreSQL · MySQL · MariaDB" },
  { name: "Native dump", detail: "pg_dump · mariadb-dump" },
  { name: "Compression", detail: "zstd / gzip" },
  { name: "Encryption", detail: "age X25519" },
  { name: "Checksum", detail: "SHA-256" },
  { name: "Upload", detail: "S3 · R2 · MinIO · Disk" },
  { name: "Verification", detail: "Restore in sandbox" },
  { name: "Metadata", detail: "Stored & audited" },
]

const FACTS = [
  { value: "1 pass", label: "Dump, compress, encrypt, hash and upload stream through connected pipes." },
  { value: "~60 MB", label: "Worker memory while backing up a 350 MB database — memory is bounded by one upload part." },
  { value: "0 partial objects", label: "If the dump or the upload fails, multipart uploads are aborted and nothing is recorded as complete." },
]

export function Architecture() {
  return (
    <Section id="architecture" labelledBy="arch-title">
      <SectionHeading
        id="arch-title"
        eyebrow="Backup architecture"
        title="A streaming pipeline built on battle-tested tools."
        description="DBVault never re-implements the dump format or invents cryptography. It connects each engine's native dump tool, zstd, age and SHA-256 into one streaming pipeline, then proves the result."
      />
      <div className="mt-14 rounded-xl border bg-card/40 p-4 sm:p-8">
        <ol className="flex flex-col items-stretch gap-2 lg:flex-row lg:items-center lg:gap-0" aria-label="Backup pipeline stages">
          {STAGES.map((s, i) => (
            <Fragment key={s.name}>
              <li className="flex items-center gap-3 rounded-lg border bg-background px-3 py-2.5 lg:min-w-0 lg:flex-1 lg:flex-col lg:items-start lg:gap-1">
                <span className="font-mono text-[10.5px] text-muted-foreground">{String(i + 1).padStart(2, "0")}</span>
                <span className="text-sm font-medium">{s.name}</span>
                <span className="ml-auto truncate text-xs text-muted-foreground lg:ml-0 lg:w-full">{s.detail}</span>
              </li>
              {i < STAGES.length - 1 && (
                <li aria-hidden className="flex justify-center text-brand lg:px-1">
                  <ArrowDown className="size-4 lg:hidden" />
                  <ArrowRight className="hidden size-3.5 lg:block" />
                </li>
              )}
            </Fragment>
          ))}
        </ol>
        <div className="mt-8 grid gap-4 border-t pt-8 sm:grid-cols-3">
          {FACTS.map((f) => (
            <div key={f.value} className="space-y-1">
              <div className="flex items-center gap-2 text-2xl font-semibold tracking-tight tabular">
                {f.value === "~60 MB" && <Cpu className="size-5 text-brand" aria-hidden />}
                {f.value}
              </div>
              <p className="text-sm text-muted-foreground">{f.label}</p>
            </div>
          ))}
        </div>
      </div>
    </Section>
  )
}
