import { Section, SectionHeading } from "./primitives"

/** Questions people actually ask before adopting a backup tool. */
export const FAQS = [
  {
    q: "Which databases does DBVault back up?",
    a: "PostgreSQL 9.2 to 18, MySQL 5.7 to 9, and MariaDB 10 and 11. Each engine is backed up with its own native tooling — pg_dump and pg_restore for PostgreSQL, mariadb-dump and the mariadb client for MySQL and MariaDB — so the artifacts are ordinary dumps you can restore without DBVault. SQL Server and SQLite are planned.",
  },
  {
    q: "Where are the backups stored?",
    a: "Wherever you already store data: Amazon S3, Cloudflare R2, MinIO, any S3-compatible service, or a directory on disk. DBVault streams the dump straight to storage in one pass, so nothing large is staged locally, and it re-reads every uploaded object to confirm its SHA-256 checksum.",
  },
  {
    q: "How does DBVault prove a backup can actually be restored?",
    a: "It restores it. Verification downloads the backup, checks its checksum, decrypts and decompresses it, restores it into a disposable sandbox database of the same engine and version, then queries every table for row counts before destroying the sandbox. If a restore test can't run, the report says so instead of claiming the backup is verified.",
  },
  {
    q: "Are backups encrypted?",
    a: "Yes. Every backup is encrypted with age (X25519 + ChaCha20-Poly1305) using a per-organization key before it leaves the worker, so your storage provider only ever sees ciphertext. Database credentials and storage keys are sealed with AES-256-GCM at rest, and secrets are never written to logs.",
  },
  {
    q: "Is DBVault free and self-hosted?",
    a: "Yes. DBVault is open source under Apache-2.0 and runs entirely on your own infrastructure. `docker compose up -d` gives you the dashboard, API, worker, scheduler, metadata database, queue and object storage. There is no phone-home and no hosted dependency.",
  },
  {
    q: "How are old backups cleaned up?",
    a: "With grandfather-father-son retention: keep N daily, N weekly and N monthly backups per schedule. DBVault shows exactly which backups a policy keeps and deletes before you apply it, and manual backups are never removed by retention.",
  },
  {
    q: "Can I use it from the terminal or from scripts?",
    a: "Yes. The dbvault CLI runs backups, restores, verifications and status checks against the same API the dashboard uses, authenticated with an API token. Every action, from the UI or the CLI, is written to an append-only audit log.",
  },
]

export function FAQ() {
  return (
    <Section id="faq" labelledBy="faq-title">
      <div className="grid gap-12 lg:grid-cols-[1fr_1.4fr] lg:items-start">
        <SectionHeading
          id="faq-title"
          eyebrow="FAQ"
          title="Questions worth asking before you trust a backup tool."
          description="If your question isn't here, the docs go deeper — and the whole thing is open source, so you can read exactly what it does."
        />
        <dl className="grid gap-4">
          {FAQS.map((f) => (
            <div key={f.q} className="rounded-xl border bg-card/50 p-5">
              <dt className="font-medium tracking-tight">{f.q}</dt>
              <dd className="mt-2 text-sm leading-relaxed text-muted-foreground">{f.a}</dd>
            </div>
          ))}
        </dl>
      </div>
    </Section>
  )
}
