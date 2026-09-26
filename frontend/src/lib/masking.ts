import type { CatalogColumn, CatalogTable, ColumnRuleValue, MaskingRule, MaskingRules } from "./types"

/** Human names and one-line explanations for each rule. */
export const RULES: { id: MaskingRule; label: string; hint: string }[] = [
  { id: "email", label: "Fake email", hint: "user_3f9a2c71@example.com, the same for the same address everywhere" },
  { id: "name", label: "Fake full name", hint: "A realistic name from a built-in list" },
  { id: "first_name", label: "Fake first name", hint: "One part of a name" },
  { id: "last_name", label: "Fake last name", hint: "One part of a name" },
  { id: "phone", label: "Fake phone", hint: "A number in the reserved 555 range" },
  { id: "hash", label: "Hash", hint: "A keyed hash: unique values stay unique" },
  { id: "date_shift", label: "Shift date", hint: "Moved by up to ±180 days, consistently per value" },
  { id: "redact", label: "Replace with", hint: "A fixed value you choose" },
  { id: "null", label: "Empty (NULL)", hint: "Only for nullable columns" },
  { id: "keep", label: "Keep (reviewed)", hint: "Copy unchanged: you checked it isn't personal" },
]

export function ruleName(v: ColumnRuleValue | undefined): MaskingRule | undefined {
  if (v === undefined) return undefined
  return typeof v === "string" ? v : "redact"
}

export function isTruncated(rules: MaskingRules, table: string): boolean {
  return rules.tables[table] === "truncate"
}

export function columnRule(rules: MaskingRules, table: string, column: string): ColumnRuleValue | undefined {
  const t = rules.tables[table]
  return t && t !== "truncate" ? t[column] : undefined
}

/** Sets (or, with undefined, clears) one column's rule. Returns new rules. */
export function setColumnRule(rules: MaskingRules, table: string, column: string, value: ColumnRuleValue | undefined): MaskingRules {
  const current = rules.tables[table]
  const cols = { ...(current && current !== "truncate" ? current : {}) }
  if (value === undefined) delete cols[column]
  else cols[column] = value
  const tables = { ...rules.tables }
  if (Object.keys(cols).length) tables[table] = cols
  else delete tables[table]
  return { tables }
}

/** Empties a table (truncate) or goes back to masking its columns. */
export function setTruncate(rules: MaskingRules, table: string, truncate: boolean): MaskingRules {
  const tables = { ...rules.tables }
  if (truncate) tables[table] = "truncate"
  else delete tables[table]
  return { tables }
}

/** Adds suggested rules where nothing was decided yet; never overrides a choice. */
export function applySuggestions(rules: MaskingRules, suggested: MaskingRules): MaskingRules {
  const tables = { ...rules.tables }
  for (const [name, s] of Object.entries(suggested.tables)) {
    const current = tables[name]
    if (current === undefined) {
      tables[name] = s
    } else if (current !== "truncate" && s !== "truncate") {
      tables[name] = { ...s, ...current }
    }
  }
  return { tables }
}

/** Personal-looking columns with no rule: a masked restore would stop on them. */
export function unruledPersonal(rules: MaskingRules, tables: CatalogTable[]): string[] {
  const out: string[] = []
  for (const t of tables) {
    if (isTruncated(rules, t.key)) continue
    for (const c of t.columns) {
      if (c.personal && columnRule(rules, t.key, c.name) === undefined) out.push(`${t.key}.${c.name}`)
    }
  }
  return out
}

const TEXT = /char|text|clob|string/i
const DATE = /date|time/i

/** Rules that can apply to a column (mirrors the server's checks). */
export function allowedRules(c: CatalogColumn): MaskingRule[] {
  if (c.key) return ["keep"]
  const text = c.type.trim() === "" || TEXT.test(c.type)
  const unique = !!c.unique
  return RULES.map((r) => r.id).filter((id) => {
    if (id === "keep") return true
    if (id === "null") return c.nullable
    if (id === "date_shift") return DATE.test(c.type) && !unique
    if (!text) return false
    if (unique) return id === "email" || id === "hash"
    return true
  })
}

/** The compact rules as YAML, for `dbvault masking apply -f`. */
export function toYAML(rules: MaskingRules): string {
  const q = (s: string) => (/^[A-Za-z0-9_.-]+$/.test(s) && !/^(true|false|null|yes|no|~)$/i.test(s) ? s : JSON.stringify(s))
  const lines = ["tables:"]
  for (const name of Object.keys(rules.tables).sort()) {
    const t = rules.tables[name]
    if (t === "truncate") {
      lines.push(`  ${q(name)}: truncate`)
      continue
    }
    lines.push(`  ${q(name)}:`)
    for (const col of Object.keys(t).sort()) {
      const v = t[col]
      lines.push(`    ${q(col)}: ${typeof v === "string" ? v : `{ redact: ${JSON.stringify(v.redact)} }`}`)
    }
  }
  if (lines.length === 1) lines[0] = "tables: {}"
  return lines.join("\n") + "\n"
}
