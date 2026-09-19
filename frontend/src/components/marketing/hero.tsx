import { ArrowRight } from "lucide-react"
import Link from "next/link"

import { Button } from "@/components/ui/button"

import { GITHUB_URL } from "./constants"
import { CopySnippet } from "./copy-snippet"
import { DashboardPreview } from "./dashboard-preview"
import { GitHubIcon } from "./github-icon"

export function Hero() {
  return (
    <section aria-labelledby="hero-title" className="relative overflow-hidden">
      <div className="pointer-events-none absolute inset-0 bg-grid mask-radial opacity-70" aria-hidden />
      <div className="pointer-events-none absolute top-24 left-1/2 h-72 w-[42rem] max-w-full -translate-x-1/2 rounded-full bg-brand/15 blur-3xl" aria-hidden />
      <div className="relative mx-auto w-full max-w-6xl px-4 pt-20 pb-16 sm:px-6 sm:pt-28 lg:px-8">
        <div className="mx-auto flex max-w-3xl flex-col items-center text-center">
          <a
            href="#open-source"
            className="animate-fade-up inline-flex items-center gap-2 rounded-full border bg-card/70 py-1 pr-3 pl-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            <span className="flex items-center gap-1.5 rounded-full bg-brand/15 px-2 py-0.5 font-medium text-brand"><span className="size-1.5 rounded-full bg-brand" />DBVault</span>
            Open source · Apache-2.0 · Self-hosted
            <ArrowRight className="size-3" />
          </a>
          <h1
            id="hero-title"
            className="animate-fade-up mt-6 text-4xl font-semibold tracking-tight text-balance sm:text-6xl lg:text-7xl"
            style={{ animationDelay: "60ms" }}
          >
            <span className="block">Open-source PostgreSQL backups</span>
            <span className="block text-brand">that just work.</span>
          </h1>
          <p className="animate-fade-up mt-6 max-w-2xl text-base text-pretty text-muted-foreground sm:text-lg" style={{ animationDelay: "120ms" }}>
            Protect your PostgreSQL databases with automated backups, encrypted storage, retention policies, restore testing, and a beautiful developer-friendly
            dashboard.
          </p>
          <div className="animate-fade-up mt-8 flex w-full flex-col items-center justify-center gap-3 sm:w-auto sm:flex-row" style={{ animationDelay: "180ms" }}>
            <Button size="lg" asChild className="h-10 w-full px-5 sm:w-auto bg-brand text-brand-foreground hover:bg-brand/90 font-semibold shadow-[0_0_24px_-6px_var(--brand)]">
              <Link href="/register">
                Get Started <ArrowRight />
              </Link>
            </Button>
            <Button size="lg" variant="outline" asChild className="h-10 w-full px-5 sm:w-auto">
              <a href={GITHUB_URL} target="_blank" rel="noreferrer">
                <GitHubIcon className="size-4" /> View on GitHub
              </a>
            </Button>
          </div>
          <div className="animate-fade-up mt-6 w-full max-w-full sm:w-auto" style={{ animationDelay: "240ms" }}>
            <CopySnippet command="docker compose up -d" />
          </div>
        </div>
        <div className="animate-fade-up mt-16 sm:mt-20" style={{ animationDelay: "320ms" }}>
          <DashboardPreview />
        </div>
      </div>
    </section>
  )
}
