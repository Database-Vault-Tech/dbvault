import { Section, SectionHeading } from "./primitives"

const STEPS = [
  {
    n: "01",
    title: "Start DBVault",
    body: "Clone the repository and bring up the stack. The dashboard is ready on localhost:3000.",
    code: "docker compose up -d",
  },
  {
    n: "02",
    title: "Connect a database",
    body: "Add host, credentials and SSL mode, then test the connection. The password is encrypted immediately and never returned by the API.",
    code: "Connection successful · PostgreSQL 17.2",
  },
  {
    n: "03",
    title: "Choose storage, schedule & retention",
    body: "Pick S3, R2, MinIO or disk, a cron schedule, and how many daily, weekly and monthly backups to keep.",
    code: "Every 6 hours · daily 7 · weekly 4 · monthly 6",
  },
  {
    n: "04",
    title: "Get verified backups",
    body: "Every backup is compressed, encrypted, checksummed and uploaded — and can be restored in a sandbox to prove it works.",
    code: "Integrity PASS · Restore test PASS",
  },
]

export function HowItWorks() {
  return (
    <Section id="how-it-works" labelledBy="how-title">
      <SectionHeading id="how-title" eyebrow="How it works" title="From zero to protected in about five minutes." />
      <ol className="mt-14 grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {STEPS.map((s, i) => (
          <li key={s.n} className="relative flex flex-col rounded-xl border bg-card/50 p-5">
            <div className="flex items-center gap-3">
              <span className="font-mono text-xs text-brand">{s.n}</span>
              {i < STEPS.length - 1 && <span className="hidden h-px flex-1 bg-border lg:block" aria-hidden />}
            </div>
            <h3 className="mt-3 font-medium">{s.title}</h3>
            <p className="mt-1.5 flex-1 text-sm leading-relaxed text-muted-foreground">{s.body}</p>
            <code className="mt-5 block truncate rounded-md border bg-background px-3 py-2 font-mono text-xs text-muted-foreground">{s.code}</code>
          </li>
        ))}
      </ol>
    </Section>
  )
}
