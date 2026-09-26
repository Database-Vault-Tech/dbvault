import GithubSlugger from "github-slugger"
import fs from "node:fs/promises"
import path from "node:path"
import { describe, expect, it } from "vitest"

import { loadDoc } from "./docs"
import { DOCS } from "./docs-nav"

describe("docs", () => {
  it("has a Markdown file for every page", async () => {
    for (const d of DOCS) {
      await expect(fs.access(path.join(process.cwd(), "..", "docs", `${d.slug}.md`)), d.slug).resolves.toBeUndefined()
    }
  })
  it("builds table-of-contents ids that match the rendered headings", async () => {
    const doc = await loadDoc("security")
    const h = doc?.headings.find((x) => x.text.includes("pg_dump"))
    expect(h?.text).toBe("Running pg_dump and pg_restore safely")
    expect(h?.id).toBe(new GithubSlugger().slug("Running pg_dump and pg_restore safely"))
    expect(doc?.body.startsWith("# ")).toBe(false)
  })
})
