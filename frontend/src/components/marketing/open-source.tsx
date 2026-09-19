import { Check } from "lucide-react"

import { Prompt, Section, Terminal } from "./primitives"

const POINTS = [
  "Runs entirely on your servers — no telemetry, no phone-home.",
  "Backups go to buckets you own; DBVault never sees them unless you host it.",
  "Apache-2.0 licensed: use it, fork it, ship it inside your product.",
  "A single Go codebase (API, worker, scheduler, CLI) and a Next.js dashboard.",
]

export function OpenSource() {
  return (
    <Section id="open-source" labelledBy="oss-title">
      <div className="grid gap-12 lg:grid-cols-2 lg:items-center">
        <div className="space-y-6">
          <p className="font-mono text-xs font-medium tracking-wider text-brand uppercase">Open source · Self-hosted</p>
          <h2 id="oss-title" className="text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
            Your database. Your backups. Your infrastructure.
          </h2>
          <p className="text-base text-pretty text-muted-foreground sm:text-lg">
            DBVault is designed to be self-hosted. Three commands give you the dashboard, API, worker, scheduler, PostgreSQL, Redis and a local S3-compatible bucket.
          </p>
          <ul className="space-y-2.5">
            {POINTS.map((p) => (
              <li key={p} className="flex gap-2.5 text-sm">
                <Check className="mt-0.5 size-4 shrink-0 text-success" aria-hidden />
                <span className="text-muted-foreground">{p}</span>
              </li>
            ))}
          </ul>
        </div>
        <div className="min-w-0 space-y-4">
          <Terminal title="~ install">
            <Prompt>git clone https://github.com/dbvault/dbvault &amp;&amp; cd dbvault</Prompt>
            <Prompt>cp .env.example .env</Prompt>
            <Prompt>docker compose up -d</Prompt>
            <span className="block text-muted-foreground">✔ Dashboard ready at http://localhost:3000</span>
          </Terminal>
          <Terminal title="~ dbvault">
            <Prompt>dbvault backup production</Prompt>
            <span className="block">&nbsp;</span>
            <span className="block">Starting backup...</span>
            <span className="block">&nbsp;</span>
            <span className="block">
              <span className="text-muted-foreground">Database:    </span>production
            </span>
            <span className="block">
              <span className="text-muted-foreground">PostgreSQL:  </span>17
            </span>
            <span className="block">
              <span className="text-muted-foreground">Destination: </span>S3
            </span>
            <span className="block">&nbsp;</span>
            <span className="block">Uploading...</span>
            <span className="block">
              <span className="text-brand">████████████████████</span> 100%
            </span>
            <span className="block">&nbsp;</span>
            <span className="block text-success">Backup completed.</span>
            <span className="block">&nbsp;</span>
            <span className="block">
              <span className="text-muted-foreground">Size:     </span>482 MB
            </span>
            <span className="block">
              <span className="text-muted-foreground">Duration: </span>2m 14s
            </span>
            <span className="block">
              <span className="text-muted-foreground">Checksum: </span>9f2c4e1a…
            </span>
          </Terminal>
        </div>
      </div>
    </Section>
  )
}
