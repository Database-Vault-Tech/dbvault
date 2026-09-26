/** Documentation pages, in reading order. Each slug is a file in the repository's docs/ folder. */
export interface DocMeta {
  slug: string
  title: string
  description: string
  group: string
}

export const DOCS: DocMeta[] = [
  { slug: "getting-started", group: "Get started", title: "Getting started", description: "Install DBVault and take your first verified, encrypted backup." },
  { slug: "configuration", group: "Get started", title: "Configuration", description: "Every environment variable: URLs, secrets, storage, workers and email." },
  { slug: "deployment", group: "Get started", title: "Deploying to a VPS", description: "Run DBVault on your own server from published images, with automatic rollback." },
  { slug: "engines", group: "Using DBVault", title: "Database engines", description: "PostgreSQL, MySQL, MariaDB and SQLite: versions, permissions and restore behavior." },
  { slug: "storage", group: "Using DBVault", title: "Storage", description: "Amazon S3, Cloudflare R2, MinIO and local disk, with IAM policies." },
  { slug: "restore", group: "Using DBVault", title: "Restore & verification", description: "Restore into a new or existing database, and prove backups restore." },
  { slug: "cli", group: "Using DBVault", title: "CLI", description: "Back up, restore and check from a terminal, a script or CI." },
  { slug: "security", group: "How it works", title: "Security", description: "Encryption, key management, two-factor sign-in, and the operator checklist." },
  { slug: "backup-engine", group: "How it works", title: "Backup engine", description: "How a dump streams through compression, encryption and checksums to storage." },
  { slug: "architecture", group: "How it works", title: "Architecture", description: "The API, worker and scheduler, the job system and scheduling guarantees." },
  { slug: "development", group: "Contributing", title: "Development", description: "Run DBVault from source, test it and contribute." },
]

export const DOC_GROUPS = [...new Set(DOCS.map((d) => d.group))]

export const GITHUB_BLOB = "https://github.com/Database-Vault-Tech/dbvault/blob/main"

/**
 * Maps a link inside a doc to where it should go on the site: other docs stay
 * on /docs, other repository files open on GitHub.
 */
export function resolveDocHref(href: string): { href: string; external: boolean } {
  if (/^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith("//")) return { href, external: !href.startsWith("mailto:") }
  if (href.startsWith("#") || href.startsWith("/")) return { href, external: false }
  const [p, hash = ""] = href.split("#")
  const anchor = hash ? `#${hash}` : ""
  const doc = /^(?:\.\/)?([\w-]+)\.md$/.exec(p)
  if (doc && DOCS.some((d) => d.slug === doc[1])) return { href: `/docs/${doc[1]}${anchor}`, external: false }
  // Anything else is relative to docs/ in the repository.
  const parts: string[] = ["docs"]
  for (const seg of p.split("/")) {
    if (seg === "..") parts.pop()
    else if (seg && seg !== ".") parts.push(seg)
  }
  const target = parts.join("/")
  if (target === "README.md") return { href: `https://github.com/Database-Vault-Tech/dbvault#readme`, external: true }
  return { href: `${GITHUB_BLOB}/${target}${anchor}`, external: true }
}
