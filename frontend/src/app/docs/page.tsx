import { ArrowRight } from "lucide-react"
import type { Metadata } from "next"
import Link from "next/link"

import { DOC_GROUPS, DOCS } from "@/lib/docs-nav"

export const metadata: Metadata = {
  title: "Documentation",
  description: "Install, configure and run DBVault: database engines, storage, restores, the CLI and security.",
  alternates: { canonical: "/docs" },
}

export default function DocsIndex() {
  return (
    <div className="max-w-4xl">
      <p className="text-sm font-medium text-brand">Documentation</p>
      <h1 className="mt-2 text-3xl font-semibold tracking-tight">Everything you need to run DBVault</h1>
      <p className="mt-3 max-w-2xl text-muted-foreground">
        Install it on your own server, protect PostgreSQL, MySQL, MariaDB and SQLite databases, and prove every backup restores.
      </p>
      <div className="mt-10 space-y-10">
        {DOC_GROUPS.map((group) => (
          <section key={group} aria-labelledby={`group-${group}`}>
            <h2 id={`group-${group}`} className="mb-3 text-sm font-medium tracking-wide text-muted-foreground uppercase">
              {group}
            </h2>
            <ul className="grid gap-3 sm:grid-cols-2">
              {DOCS.filter((d) => d.group === group).map((d) => (
                <li key={d.slug}>
                  <Link
                    href={`/docs/${d.slug}`}
                    className="group flex h-full flex-col rounded-xl border bg-card p-4 transition-colors hover:border-brand/50 hover:bg-brand/5"
                  >
                    <span className="flex items-center justify-between gap-2 font-medium">
                      {d.title}
                      <ArrowRight className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-brand" />
                    </span>
                    <span className="mt-1 text-sm text-muted-foreground">{d.description}</span>
                  </Link>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </div>
  )
}
