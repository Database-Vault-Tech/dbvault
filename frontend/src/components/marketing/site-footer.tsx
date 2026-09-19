import Link from "next/link"

import { Logo } from "@/components/brand/logo"

import { DISCUSSIONS_URL, DOCS_URL, GITHUB_URL } from "./constants"

const COLUMNS = [
  {
    title: "Product",
    links: [
      { label: "Features", href: "#features" },
      { label: "How it works", href: "#how-it-works" },
      { label: "Security", href: "#security" },
      { label: "Pricing", href: "#pricing" },
      { label: "Sign in", href: "/login" },
    ],
  },
  {
    title: "Docs",
    links: [
      { label: "README", href: DOCS_URL },
      { label: "Architecture", href: `${GITHUB_URL}/blob/main/docs/architecture.md` },
      { label: "Storage", href: `${GITHUB_URL}/blob/main/docs/storage.md` },
      { label: "Security", href: `${GITHUB_URL}/blob/main/docs/security.md` },
      { label: "Restore", href: `${GITHUB_URL}/blob/main/docs/restore.md` },
    ],
  },
  {
    title: "Community",
    links: [
      { label: "GitHub", href: GITHUB_URL },
      { label: "Discussions", href: DISCUSSIONS_URL },
      { label: "Issues", href: `${GITHUB_URL}/issues` },
      { label: "Contributing", href: `${GITHUB_URL}/blob/main/CONTRIBUTING.md` },
      { label: "Security policy", href: `${GITHUB_URL}/blob/main/SECURITY.md` },
    ],
  },
]

export function SiteFooter() {
  return (
    <footer className="border-t border-border/60">
      <div className="mx-auto grid w-full max-w-6xl gap-10 px-4 py-14 sm:px-6 md:grid-cols-[1.4fr_repeat(3,1fr)] lg:px-8">
        <div className="space-y-3">
          <Logo />
          <p className="max-w-xs text-sm text-muted-foreground">Open-source PostgreSQL backups that just work.</p>
        </div>
        {COLUMNS.map((c) => (
          <nav key={c.title} aria-label={c.title} className="space-y-3">
            <h3 className="text-sm font-medium">{c.title}</h3>
            <ul className="space-y-2">
              {c.links.map((l) => (
                <li key={l.label}>
                  {l.href.startsWith("/") || l.href.startsWith("#") ? (
                    <Link href={l.href} className="text-sm text-muted-foreground transition-colors hover:text-foreground">
                      {l.label}
                    </Link>
                  ) : (
                    <a href={l.href} target="_blank" rel="noreferrer" className="text-sm text-muted-foreground transition-colors hover:text-foreground">
                      {l.label}
                    </a>
                  )}
                </li>
              ))}
            </ul>
          </nav>
        ))}
      </div>
      <div className="border-t border-border/60">
        <div className="mx-auto flex w-full max-w-6xl flex-col gap-2 px-4 py-6 text-xs text-muted-foreground sm:flex-row sm:justify-between sm:px-6 lg:px-8">
          <span>© {new Date().getFullYear()} DBVault contributors</span>
          <a href={`${GITHUB_URL}/blob/main/LICENSE`} target="_blank" rel="noreferrer" className="hover:text-foreground">
            Licensed under Apache-2.0
          </a>
        </div>
      </div>
    </footer>
  )
}
