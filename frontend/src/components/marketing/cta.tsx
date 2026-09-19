import { ArrowRight, Star } from "lucide-react"
import Link from "next/link"

import { Button } from "@/components/ui/button"

import { GITHUB_URL } from "./constants"

export function GitHubCTA() {
  return (
    <section aria-labelledby="cta-title" className="border-t border-border/60">
      <div className="mx-auto w-full max-w-6xl px-4 py-20 sm:px-6 lg:px-8">
        <div className="relative overflow-hidden rounded-2xl border bg-card px-6 py-14 text-center sm:px-12">
          <div className="pointer-events-none absolute inset-0 bg-grid mask-radial opacity-50" aria-hidden />
          <div className="relative mx-auto max-w-2xl space-y-5">
            <h2 id="cta-title" className="text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
              Stop hoping your backups work.
            </h2>
            <p className="text-muted-foreground sm:text-lg">
              Protect your first PostgreSQL database in minutes — and prove it restores.
            </p>
            <div className="flex flex-col items-center justify-center gap-3 sm:flex-row">
              <Button size="lg" asChild className="h-10 w-full px-5 sm:w-auto bg-brand text-brand-foreground hover:bg-brand/90 font-semibold">
                <a href={GITHUB_URL} target="_blank" rel="noreferrer">
                  <Star /> Star on GitHub
                </a>
              </Button>
              <Button size="lg" variant="outline" asChild className="h-10 w-full px-5 sm:w-auto">
                <Link href="/register">
                  Get started <ArrowRight />
                </Link>
              </Button>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
