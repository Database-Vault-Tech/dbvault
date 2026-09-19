import { Check } from "lucide-react"
import Link from "next/link"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

import { DISCUSSIONS_URL, GITHUB_URL } from "./constants"
import { Section, SectionHeading } from "./primitives"

const PLANS = [
  {
    name: "Self-hosted",
    price: "Free",
    period: "forever",
    description: "The complete product, open source, on your own infrastructure.",
    features: ["Unlimited databases and backups", "S3, R2, MinIO and local storage", "Encryption, verification and restore", "Teams, roles and audit log", "CLI and REST API"],
    cta: { label: "Get started", href: "/register", internal: true },
    highlight: true,
  },
  {
    name: "DBVault Cloud",
    price: "Coming soon",
    period: "",
    description: "The same engine, operated for you — with your own buckets or ours.",
    features: ["Managed workers and upgrades", "Multi-region backup storage", "Team billing and usage insights", "SSO / SAML", "Advanced monitoring"],
    cta: { label: "Join the waitlist discussion", href: DISCUSSIONS_URL, internal: false },
    badge: "Coming soon",
  },
  {
    name: "Enterprise",
    price: "Custom",
    period: "",
    description: "Support, compliance help and guidance for large fleets.",
    features: ["Priority support", "Deployment reviews", "Custom retention and key management", "Security questionnaires"],
    cta: { label: "Contact us", href: GITHUB_URL, internal: false },
  },
]

export function Pricing() {
  return (
    <Section id="pricing" labelledBy="pricing-title">
      <SectionHeading
        id="pricing-title"
        eyebrow="Pricing"
        title="Free to self-host. A managed cloud is on the way."
        description="Every feature is in the open-source edition. The hosted version will add convenience, not paywalls on safety."
      />
      <div className="mt-14 grid gap-4 lg:grid-cols-3">
        {PLANS.map((p) => (
          <div key={p.name} className={cn("flex flex-col rounded-xl border bg-card/50 p-6", p.highlight && "border-brand/50 bg-card ring-1 ring-brand/20")}>
            <div className="flex items-center justify-between gap-2">
              <h3 className="font-medium">{p.name}</h3>
              {p.badge && <Badge variant="outline">{p.badge}</Badge>}
            </div>
            <div className="mt-4 flex items-baseline gap-1.5">
              <span className="text-3xl font-semibold tracking-tight">{p.price}</span>
              {p.period && <span className="text-sm text-muted-foreground">{p.period}</span>}
            </div>
            <p className="mt-2 text-sm text-muted-foreground">{p.description}</p>
            <ul className="mt-6 flex-1 space-y-2.5">
              {p.features.map((f) => (
                <li key={f} className="flex gap-2 text-sm">
                  <Check className="mt-0.5 size-4 shrink-0 text-brand" aria-hidden />
                  {f}
                </li>
              ))}
            </ul>
            <Button asChild variant={p.highlight ? "default" : "outline"} className={cn("mt-8 h-9", p.highlight && "bg-brand text-brand-foreground hover:bg-brand/90 font-semibold")}>
              {p.cta.internal ? (
                <Link href={p.cta.href}>{p.cta.label}</Link>
              ) : (
                <a href={p.cta.href} target="_blank" rel="noreferrer">
                  {p.cta.label}
                </a>
              )}
            </Button>
          </div>
        ))}
      </div>
    </Section>
  )
}
