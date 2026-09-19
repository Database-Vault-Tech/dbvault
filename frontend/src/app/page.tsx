import type { Metadata } from "next"

import { SITE_DESCRIPTION, SITE_NAME, SITE_TAGLINE, SITE_URL } from "@/lib/site"

import { Architecture } from "@/components/marketing/architecture"
import { GitHubCTA } from "@/components/marketing/cta"
import { FAQ } from "@/components/marketing/faq"
import { Features } from "@/components/marketing/features"
import { Hero } from "@/components/marketing/hero"
import { HowItWorks } from "@/components/marketing/how-it-works"
import { OpenSource } from "@/components/marketing/open-source"
import { Pricing } from "@/components/marketing/pricing"
import { Security } from "@/components/marketing/security"
import { SiteFooter } from "@/components/marketing/site-footer"
import { SiteHeader } from "@/components/marketing/site-header"
import { StorageProviders } from "@/components/marketing/storage-providers"
import { StructuredData } from "@/components/marketing/structured-data"
import { SupportedDatabases } from "@/components/marketing/supported-databases"

export const metadata: Metadata = {
  title: { absolute: `${SITE_NAME} — ${SITE_TAGLINE}` },
  description: SITE_DESCRIPTION,
  alternates: { canonical: "/" },
  openGraph: { url: SITE_URL, title: `${SITE_NAME} — ${SITE_TAGLINE}`, description: SITE_DESCRIPTION },
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
        <FAQ />
        <GitHubCTA />
      </main>
      <SiteFooter />
      <StructuredData />
    </div>
  )
}
