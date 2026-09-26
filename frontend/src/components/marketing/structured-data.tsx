import { GITHUB_URL } from "@/components/marketing/constants"
import { FAQS } from "@/components/marketing/faq"
import { SITE_DESCRIPTION, SITE_NAME, SITE_TAGLINE, SITE_URL } from "@/lib/site"

/**
 * Schema.org data for the landing page: what the software is, who publishes
 * it, and the questions it answers. Search engines use this for rich
 * results, so everything here must match what the page actually says.
 */
export function StructuredData() {
  const graph = [
    {
      "@type": "SoftwareApplication",
      "@id": `${SITE_URL}/#software`,
      name: SITE_NAME,
      alternateName: "DBVault backup platform",
      description: SITE_DESCRIPTION,
      applicationCategory: "DeveloperApplication",
      applicationSubCategory: "Database backup software",
      operatingSystem: "Linux, macOS, Windows (Docker)",
      softwareRequirements: "Docker with Compose v2",
      url: SITE_URL,
      license: "https://www.apache.org/licenses/LICENSE-2.0",
      isAccessibleForFree: true,
      offers: { "@type": "Offer", price: "0", priceCurrency: "USD", availability: "https://schema.org/InStock" },
      featureList: [
        "Automated PostgreSQL, MySQL, MariaDB and SQLite backups",
        "Scheduled backups with cron expressions and timezones",
        "End-to-end encryption with age (X25519 + ChaCha20-Poly1305)",
        "SHA-256 checksums verified after upload and before restore",
        "Restore testing in a disposable sandbox database",
        "Grandfather-father-son retention policies",
        "Amazon S3, Cloudflare R2, MinIO and local disk storage",
        "Email and webhook notifications",
        "Organizations, roles and an append-only audit log",
        "Command-line interface and REST API",
      ],
      codeRepository: GITHUB_URL,
      sameAs: [GITHUB_URL],
    },
    {
      "@type": "Organization",
      "@id": `${SITE_URL}/#organization`,
      name: SITE_NAME,
      url: SITE_URL,
      logo: `${SITE_URL}/icon.svg`,
      sameAs: [GITHUB_URL],
    },
    {
      "@type": "WebSite",
      "@id": `${SITE_URL}/#website`,
      url: SITE_URL,
      name: SITE_NAME,
      description: SITE_TAGLINE,
      publisher: { "@id": `${SITE_URL}/#organization` },
      inLanguage: "en",
    },
    {
      "@type": "FAQPage",
      "@id": `${SITE_URL}/#faq`,
      mainEntity: FAQS.map((f) => ({
        "@type": "Question",
        name: f.q,
        acceptedAnswer: { "@type": "Answer", text: f.a },
      })),
    },
  ]

  return (
    <script
      type="application/ld+json"
      // Static, first-party data built above — no user input reaches this.
      dangerouslySetInnerHTML={{ __html: JSON.stringify({ "@context": "https://schema.org", "@graph": graph }) }}
    />
  )
}
