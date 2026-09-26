import { ArrowLeft, ArrowRight, Pencil } from "lucide-react"
import type { Metadata } from "next"
import Link from "next/link"
import { notFound } from "next/navigation"

import { Markdown } from "@/components/docs/markdown"
import { loadDoc } from "@/lib/docs"
import { DOCS, GITHUB_BLOB } from "@/lib/docs-nav"
import { cn } from "@/lib/utils"

// Every page is rendered at build time from docs/*.md; unknown slugs are 404s.
export const dynamicParams = false

export function generateStaticParams() {
  return DOCS.map((d) => ({ slug: d.slug }))
}

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
  const { slug } = await params
  const meta = DOCS.find((d) => d.slug === slug)
  if (!meta) return {}
  return { title: meta.title, description: meta.description, alternates: { canonical: `/docs/${slug}` } }
}

export default async function DocPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params
  const doc = await loadDoc(slug)
  if (!doc) notFound()
  const i = DOCS.findIndex((d) => d.slug === slug)
  const prev = DOCS[i - 1]
  const next = DOCS[i + 1]
  const toc = doc.headings.filter((h) => h.depth === 2)

  return (
    <div className="flex gap-10">
      <article className="min-w-0 max-w-3xl flex-1">
        <p className="text-sm font-medium text-brand">{doc.group}</p>
        <h1 className="mt-2 text-3xl font-semibold tracking-tight">{doc.title}</h1>
        <p className="mt-2 text-muted-foreground">{doc.description}</p>
        <Markdown className="mt-6">{doc.body}</Markdown>

        <div className="mt-12 flex items-center justify-between gap-4 border-t pt-6 text-sm">
          <a
            href={`${GITHUB_BLOB}/docs/${slug}.md`}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1.5 text-muted-foreground hover:text-foreground"
          >
            <Pencil className="size-3.5" /> Edit this page on GitHub
          </a>
        </div>
        <nav aria-label="Previous and next" className="mt-6 grid gap-3 sm:grid-cols-2">
          {prev ? (
            <Link href={`/docs/${prev.slug}`} className="rounded-lg border p-3 transition-colors hover:border-brand/50">
              <span className="flex items-center gap-1 text-xs text-muted-foreground">
                <ArrowLeft className="size-3" /> Previous
              </span>
              <span className="font-medium">{prev.title}</span>
            </Link>
          ) : (
            <span />
          )}
          {next && (
            <Link href={`/docs/${next.slug}`} className="rounded-lg border p-3 text-right transition-colors hover:border-brand/50">
              <span className="flex items-center justify-end gap-1 text-xs text-muted-foreground">
                Next <ArrowRight className="size-3" />
              </span>
              <span className="font-medium">{next.title}</span>
            </Link>
          )}
        </nav>
      </article>

      {toc.length > 1 && (
        <aside className="sticky top-24 hidden h-fit max-h-[calc(100vh-8rem)] w-56 shrink-0 overflow-y-auto xl:block">
          <p className="mb-2 text-xs font-medium tracking-wide text-muted-foreground/80 uppercase">On this page</p>
          <ul className="space-y-1 border-l text-sm">
            {doc.headings.map((h) => (
              <li key={h.id}>
                <a
                  href={`#${h.id}`}
                  className={cn("-ml-px block border-l border-transparent py-0.5 text-muted-foreground hover:border-foreground/40 hover:text-foreground", h.depth === 3 ? "pl-6" : "pl-3")}
                >
                  {h.text}
                </a>
              </li>
            ))}
          </ul>
        </aside>
      )}
    </div>
  )
}
