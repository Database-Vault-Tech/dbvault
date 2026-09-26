import { describe, expect, it } from "vitest"

import { allowedRules, applySuggestions, columnRule, setColumnRule, setTruncate, toYAML, unruledPersonal } from "./masking"
import type { CatalogTable, MaskingRules } from "./types"

const tables: CatalogTable[] = [
  {
    name: "users",
    key: "users",
    suggest_truncate: false,
    columns: [
      { name: "id", type: "integer", nullable: false, key: true, personal: false },
      { name: "email", type: "text", nullable: false, unique: true, personal: true, suggested: "email" },
      { name: "phone", type: "text", nullable: true, personal: true, suggested: "phone" },
      { name: "plan", type: "text", nullable: false, personal: false },
    ],
  },
  { name: "sessions", key: "sessions", suggest_truncate: true, columns: [{ name: "ip", type: "text", nullable: true, personal: true, suggested: "hash" }] },
]

describe("masking rules", () => {
  it("sets and clears column rules without mutating", () => {
    const empty: MaskingRules = { tables: {} }
    const a = setColumnRule(empty, "users", "email", "email")
    const b = setColumnRule(a, "users", "phone", { redact: "x" })
    expect(empty.tables).toEqual({})
    expect(columnRule(b, "users", "phone")).toEqual({ redact: "x" })
    expect(setColumnRule(a, "users", "email", undefined).tables).toEqual({})
    expect(setTruncate(b, "users", true).tables.users).toBe("truncate")
    expect(setTruncate(setTruncate(b, "users", true), "users", false).tables.users).toBeUndefined()
  })

  it("applies suggestions without overriding decisions", () => {
    const mine: MaskingRules = { tables: { users: { email: "hash" } } }
    const suggested: MaskingRules = { tables: { users: { email: "email", phone: "phone" }, sessions: "truncate" } }
    expect(applySuggestions(mine, suggested)).toEqual({ tables: { users: { email: "hash", phone: "phone" }, sessions: "truncate" } })
  })

  it("finds personal columns nobody decided about", () => {
    expect(unruledPersonal({ tables: { users: { email: "email" } } }, tables)).toEqual(["users.phone", "sessions.ip"])
    expect(unruledPersonal({ tables: { users: { email: "email", phone: "keep" }, sessions: "truncate" } }, tables)).toEqual([])
  })

  it("offers only rules the column can take", () => {
    const [id, email, phone] = tables[0].columns
    expect(allowedRules(id)).toEqual(["keep"])
    expect(allowedRules(email)).toEqual(["email", "hash", "keep"])
    expect(allowedRules(phone)).toContain("null")
    expect(allowedRules({ name: "dob", type: "date", nullable: true, personal: true })).toEqual(["date_shift", "null", "keep"])
  })

  it("exports YAML the CLI can apply", () => {
    expect(toYAML({ tables: { users: { email: "email", "full name": { redact: 'a "b"' } }, sessions: "truncate" } })).toBe(
      'tables:\n  sessions: truncate\n  users:\n    email: email\n    "full name": { redact: "a \\"b\\"" }\n',
    )
    expect(toYAML({ tables: {} })).toBe("tables: {}\n")
  })
})
