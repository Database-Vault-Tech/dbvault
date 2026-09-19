import type { Metadata } from "next"

import { Architecture } from "@/components/marketing/architecture"
import { GitHubCTA } from "@/components/marketing/cta"
import { Features } from "@/components/marketing/features"
import { Hero } from "@/components/marketing/hero"
import { HowItWorks } from "@/components/marketing/how-it-works"
import { OpenSource } from "@/components/marketing/open-source"
import { Pricing } from "@/components/marketing/pricing"
import { Security } from "@/components/marketing/security"
import { SiteFooter } from "@/components/marketing/site-footer"
import { SiteHeader } from "@/components/marketing/site-header"
import { StorageProviders } from "@/components/marketing/storage-providers"
import { SupportedDatabases } from "@/components/marketing/supported-databases"

export const metadata: Metadata = {
  title: { absolute: "DBVault — Open-source SQL database backups that just work" },
  description:
    "Protect your SQL databases (PostgreSQL today; MySQL, MariaDB, SQL Server and SQLite coming) with automated backups, encrypted storage, retention policies, restore testing, and a beautiful developer-friendly dashboard. Self-hosted and Apache-2.0.",
}

export default function Home() {
  return (
    <div className="min-h-screen overflow-x-clip bg-background text-foreground">
      <SiteHeader />
      <main>
        <Hero />
        <Features />
        <SupportedDatabases />
        <HowItWorks />
        <Architecture />
        <StorageProviders />
        <Security />
        <OpenSource />
        <Pricing />
        <GitHubCTA />
      </main>
      <SiteFooter />
    </div>
  )
}
