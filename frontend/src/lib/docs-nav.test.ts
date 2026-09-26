import { describe, expect, it } from "vitest"

import { resolveDocHref } from "./docs-nav"

describe("resolveDocHref", () => {
  it("keeps links between docs on the site", () => {
    expect(resolveDocHref("engines.md")).toEqual({ href: "/docs/engines", external: false })
    expect(resolveDocHref("./storage.md#minio")).toEqual({ href: "/docs/storage#minio", external: false })
    expect(resolveDocHref("#sqlite")).toEqual({ href: "#sqlite", external: false })
  })
  it("sends other repository files to GitHub", () => {
    expect(resolveDocHref("../SECURITY.md")).toEqual({ href: "https://github.com/Database-Vault-Tech/dbvault/blob/main/SECURITY.md", external: true })
    expect(resolveDocHref("../.env.example")).toEqual({ href: "https://github.com/Database-Vault-Tech/dbvault/blob/main/.env.example", external: true })
    expect(resolveDocHref("../backend/internal/backups/engine.go")).toMatchObject({ href: expect.stringContaining("/blob/main/backend/internal/backups/engine.go") })
    expect(resolveDocHref("../README.md")).toEqual({ href: "https://github.com/Database-Vault-Tech/dbvault#readme", external: true })
    expect(resolveDocHref("unknown.md").href).toBe("https://github.com/Database-Vault-Tech/dbvault/blob/main/docs/unknown.md")
  })
  it("leaves absolute URLs alone", () => {
    expect(resolveDocHref("https://age-encryption.org")).toEqual({ href: "https://age-encryption.org", external: true })
    expect(resolveDocHref("mailto:security@example.com").external).toBe(false)
  })
})
