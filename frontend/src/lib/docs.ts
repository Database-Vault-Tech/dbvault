import GithubSlugger from "github-slugger"
import fs from "node:fs/promises"
import path from "node:path"

import { DOCS, type DocMeta } from "./docs-nav"

// The Markdown in the repository's docs/ folder is the single source for the
// documentation site and GitHub. Pages are rendered at build time, so the
// files are only read by `next build` (../docs from frontend/, or the "docs"
// build context copied to /docs in the Docker image).
const DOCS_DIR = process.env.DOCS_DIR ?? path.join(process.cwd(), "..", "docs")

export interface Heading {
  depth: 2 | 3
  text: string
  id: string
}

export interface Doc extends DocMeta {
  /** Markdown without the leading "# Title" line. */
  body: string
  headings: Heading[]
}

/** Plain text of inline Markdown, for the table of contents. */
function plain(md: string): string {
  return md
    .replace(/`([^`]*)`/g, "$1")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    // Emphasis markers only: underscores inside words (pg_dump) are text.
    .replace(/(\*\*|__|~~)(.+?)\1/g, "$2")
    .replace(/(^|[^\w*])[*_](\S(?:.*?\S)?)[*_](?=[^\w*]|$)/g, "$1$2")
    .trim()
}

export async function loadDoc(slug: string): Promise<Doc | null> {
  const meta = DOCS.find((d) => d.slug === slug)
  if (!meta) return null
  const raw = await fs.readFile(path.join(DOCS_DIR, `${slug}.md`), "utf8")
  const body = raw.replace(/^# .*\r?\n/, "").trim()

  // Same slugs rehype-slug gives the rendered headings (the title is
  // stripped from the body before either sees it).
  const slugger = new GithubSlugger()
  const headings: Heading[] = []
  let inFence = false
  for (const line of body.split(/\r?\n/)) {
    if (/^\s*(```|~~~)/.test(line)) inFence = !inFence
    if (inFence) continue
    const m = /^(#{2,6})\s+(.*?)\s*#*$/.exec(line)
    if (!m) continue
    const text = plain(m[2])
    const id = slugger.slug(text)
    if (m[1].length <= 3) headings.push({ depth: m[1].length as 2 | 3, text, id })
  }
  return { ...meta, body, headings }
}
