import { describe, expect, it } from "vitest"

import { safeNext } from "./auth-form"

describe("safeNext (open-redirect protection)", () => {
  it("allows same-site paths", () => {
    expect(safeNext("/backups/123?tab=logs")).toBe("/backups/123?tab=logs")
  })
  it.each(["https://evil.example", "//evil.example", "/\\evil.example", "javascript:alert(1)", "", null])("rejects %s", (v) => {
    expect(safeNext(v as string | null)).toBe("/dashboard")
  })
})
