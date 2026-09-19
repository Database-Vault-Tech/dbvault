import { EyeOff, FileKey, KeyRound, ListChecks, ShieldCheck, Timer, UserCheck, Users } from "lucide-react"

import { Prompt, Section, SectionHeading, Terminal } from "./primitives"

const ITEMS = [
  { icon: FileKey, title: "Encryption at rest", body: "Backups use age; database passwords and S3 keys are sealed with AES-256-GCM bound to their row." },
  { icon: EyeOff, title: "Credentials never returned", body: "Once stored, passwords and secret keys never appear in API responses — only masked hints." },
  { icon: KeyRound, title: "argon2id passwords", body: "OWASP-recommended parameters, HttpOnly sessions and hashed API tokens." },
  { icon: ShieldCheck, title: "CSRF protection", body: "Signed double-submit tokens, strict Origin checks and JSON-only mutations." },
  { icon: Timer, title: "Rate limiting", body: "Redis-backed limits on sign-in and the API, per IP and per account." },
  { icon: Users, title: "Roles", body: "Owner, admin, member and viewer permissions, scoped per organization." },
  { icon: ListChecks, title: "Audit logs", body: "Every database, storage, backup, restore and team change is recorded." },
  { icon: UserCheck, title: "No secrets in logs", body: "Structured JSON logs redact passwords, tokens and keys automatically." },
]

export function Security() {
  return (
    <Section id="security" labelledBy="security-title">
      <SectionHeading
        id="security-title"
        eyebrow="Security"
        title="Built like the financial infrastructure it protects."
        description="Your backups contain everything. DBVault treats them that way — and never locks you in."
      />
      <div className="mt-14 grid gap-10 lg:grid-cols-[1.3fr_1fr]">
        <ul className="grid gap-x-8 gap-y-6 sm:grid-cols-2">
          {ITEMS.map((it) => (
            <li key={it.title} className="flex gap-3">
              <it.icon className="mt-0.5 size-4 shrink-0 text-brand" aria-hidden />
              <div>
                <h3 className="text-sm font-medium">{it.title}</h3>
                <p className="mt-1 text-sm leading-relaxed text-muted-foreground">{it.body}</p>
              </div>
            </li>
          ))}
        </ul>
        <div className="min-w-0 space-y-4">
          <h3 className="font-medium">Your recovery key works without DBVault.</h3>
          <p className="text-sm leading-relaxed text-muted-foreground">
            Owners can export the organization&apos;s recovery key. Backups are standard age files, so you can decrypt and restore them with open-source tools even if
            DBVault itself is gone.
          </p>
          <Terminal title="disaster recovery">
            <Prompt>
              age -d -i recovery.key backup.dump.zst.age | zstd -d | pg_restore -d mydb
            </Prompt>
          </Terminal>
        </div>
      </div>
    </Section>
  )
}
