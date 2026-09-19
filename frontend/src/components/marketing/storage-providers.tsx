import { Cloud, HardDrive, Server } from "lucide-react"

import { Section, SectionHeading } from "./primitives"

const PROVIDERS = [
  { name: "Amazon S3", icon: Cloud, body: "Any region, any bucket. Streaming multipart uploads with adaptive part sizes beyond 1 TB." },
  { name: "Cloudflare R2", icon: Cloud, body: "Just your account ID, bucket and API token. No egress fees for restores." },
  { name: "MinIO", icon: Server, body: "Self-hosted S3 on your own hardware. Bundled with docker compose for local development." },
  { name: "Local filesystem", icon: HardDrive, body: "Atomic writes confined to a configured root directory, with path traversal protection." },
]

export function StorageProviders() {
  return (
    <Section id="storage" labelledBy="storage-title">
      <div className="grid gap-12 lg:grid-cols-[1fr_1.4fr] lg:items-start">
        <SectionHeading
          id="storage-title"
          eyebrow="Storage providers"
          title="Store backups wherever you already trust."
          description="One storage abstraction, four destinations. Credentials are encrypted at rest and every destination is tested with a real write, read and delete before you rely on it."
        />
        <ul className="grid gap-3 sm:grid-cols-2">
          {PROVIDERS.map((p) => (
            <li key={p.name} className="rounded-xl border bg-card/50 p-5">
              <div className="flex items-center gap-2.5">
                <p.icon className="size-4 text-brand" aria-hidden />
                <span className="font-medium tracking-tight">{p.name}</span>
              </div>
              <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{p.body}</p>
            </li>
          ))}
        </ul>
      </div>
    </Section>
  )
}
