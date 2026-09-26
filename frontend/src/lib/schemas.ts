import { z } from "zod"

import { ENGINE_LIST, ENGINES, type SupportedEngine } from "@/lib/engines"

export const passwordSchema = z
  .string()
  .min(10, "Use at least 10 characters.")
  .max(256, "Use at most 256 characters.")

export const loginSchema = z.object({
  email: z.email("Enter a valid email address."),
  password: z.string().min(1, "Enter your password."),
})
export type LoginValues = z.infer<typeof loginSchema>

export const registerSchema = z.object({
  name: z.string().trim().min(1, "Enter your name.").max(100),
  email: z.email("Enter a valid email address."),
  password: passwordSchema,
})
export type RegisterValues = z.infer<typeof registerSchema>

export const sslModes = ["disable", "allow", "prefer", "require", "verify-ca", "verify-full"] as const

// Hostname, IPv4, or IPv6 (at least two colons, so "db:5432" is rejected:
// ports go in their own field).
const hostRegex =
  /^(\[?[0-9a-fA-F]{0,4}(:[0-9a-fA-F]{0,4}){2,7}(:\d{1,3}(\.\d{1,3}){3})?\]?|[a-zA-Z0-9]([a-zA-Z0-9\-_]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-_]{0,61}[a-zA-Z0-9])?)*\.?)$/

const engineIds = ENGINE_LIST.map((e) => e.id) as [SupportedEngine, ...SupportedEngine[]]

/** A path inside the SQLite folder (mirrors the API's validate.IsRelativeFilePath). */
export const SQLITE_PATH_RE = /^[A-Za-z0-9._\- +@()]+(\/[A-Za-z0-9._\- +@()]+)*$/

export function isSQLitePath(p: string): boolean {
  return (
    p.length > 0 &&
    p.length <= 255 &&
    SQLITE_PATH_RE.test(p) &&
    p.split("/").every((seg) => seg !== "." && seg !== ".." && !seg.startsWith("-") && seg.trim() === seg)
  )
}

export function databaseSchema(requirePassword: boolean) {
  return z
    .object({
      engine: z.enum(engineIds),
      name: z
        .string()
        .trim()
        .min(1, "Give this database a name.")
        .max(63)
        .regex(/^[a-zA-Z0-9][a-zA-Z0-9 _.-]*$/, "Use letters, numbers, spaces, dots, dashes or underscores."),
      host: z.string().trim(),
      port: z.coerce.number<number>().int(),
      database: z.string().trim(),
      username: z.string().trim(),
      password: z.string().optional(),
      ssl_mode: z.enum(sslModes),
      ssl_root_cert: z.string().optional(),
    })
    .superRefine((v, ctx) => {
      const issue = (path: string, message: string) => ctx.addIssue({ code: "custom", path: [path], message })
      // SQLite: just a file path inside the mounted folder.
      if (ENGINES[v.engine].fileBased) {
        if (!v.database) issue("database", "Enter the path of the database file.")
        else if (!isSQLitePath(v.database)) issue("database", "Use a path inside the SQLite folder, like app/data.db (no leading /, no ..).")
        return
      }
      if (!v.host) issue("host", "Enter the host.")
      else if (!hostRegex.test(v.host)) issue("host", "Enter a hostname or IP address (no port or scheme).")
      if (v.port < 1 || v.port > 65535) issue("port", "Enter a port between 1 and 65535.")
      if (!v.database) issue("database", "Enter the database name.")
      else if (v.database.length > 63) issue("database", "Use at most 63 characters.")
      else if (v.database.startsWith("-")) issue("database", "Database names can't start with a dash.")
      if (!v.username) issue("username", "Enter the username.")
      else if (v.username.length > 63) issue("username", "Use at most 63 characters.")
      if (requirePassword && !v.password) issue("password", "Enter the password.")
      if (!(ENGINES[v.engine].sslModes as readonly string[]).includes(v.ssl_mode)) issue("ssl_mode", "This SSL mode isn't available for this database type.")
    })
}
export type DatabaseValues = z.infer<ReturnType<typeof databaseSchema>>

// MySQL connectors spell SSL modes differently (ssl-mode=REQUIRED).
const MYSQL_SSL: Record<string, DatabaseValues["ssl_mode"]> = {
  disabled: "disable",
  preferred: "prefer",
  required: "require",
  verify_ca: "verify-full",
  verify_identity: "verify-full",
}

/**
 * Parses a connection string into form values, so users can paste
 * "postgres://user:pass@host:5432/db?sslmode=require" or
 * "mysql://user:pass@host:3306/db".
 */
export function parseConnectionString(raw: string): Partial<DatabaseValues> | null {
  try {
    const url = new URL(raw.trim())
    const engine = ENGINE_LIST.find((e) => e.urlSchemes.includes(url.protocol))
    if (!engine) return null
    let ssl = url.searchParams.get("sslmode") ?? ""
    const mysqlSSL = url.searchParams.get("ssl-mode") ?? url.searchParams.get("ssl_mode")
    if (mysqlSSL) ssl = MYSQL_SSL[mysqlSSL.toLowerCase()] ?? ""
    return {
      engine: engine.id,
      host: url.hostname,
      port: url.port ? Number(url.port) : engine.defaultPort,
      database: decodeURIComponent(url.pathname.replace(/^\//, "")) || engine.defaultDatabase,
      username: decodeURIComponent(url.username),
      password: decodeURIComponent(url.password),
      ...((engine.sslModes as readonly string[]).includes(ssl) ? { ssl_mode: ssl as DatabaseValues["ssl_mode"] } : {}),
    }
  } catch {
    return null
  }
}
