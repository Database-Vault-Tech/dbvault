import { z } from "zod"

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

export function databaseSchema(requirePassword: boolean) {
  return z.object({
    name: z
      .string()
      .trim()
      .min(1, "Give this database a name.")
      .max(63)
      .regex(/^[a-zA-Z0-9][a-zA-Z0-9 _.-]*$/, "Use letters, numbers, spaces, dots, dashes or underscores."),
    host: z.string().trim().min(1, "Enter the host.").regex(hostRegex, "Enter a hostname or IP address (no port or scheme)."),
    port: z.coerce.number<number>().int().min(1).max(65535),
    database: z.string().trim().min(1, "Enter the database name.").max(63),
    username: z.string().trim().min(1, "Enter the username.").max(63),
    password: requirePassword ? z.string().min(1, "Enter the password.") : z.string().optional(),
    ssl_mode: z.enum(sslModes),
    ssl_root_cert: z.string().optional(),
  })
}
export type DatabaseValues = z.infer<ReturnType<typeof databaseSchema>>

/**
 * Parses a PostgreSQL connection string into form values, so users can paste
 * "postgres://user:pass@host:5432/db?sslmode=require".
 */
export function parseConnectionString(raw: string): Partial<DatabaseValues> | null {
  try {
    const url = new URL(raw.trim())
    if (url.protocol !== "postgres:" && url.protocol !== "postgresql:") return null
    const sslmode = url.searchParams.get("sslmode") as DatabaseValues["ssl_mode"] | null
    return {
      host: url.hostname,
      port: url.port ? Number(url.port) : 5432,
      database: decodeURIComponent(url.pathname.replace(/^\//, "")) || "postgres",
      username: decodeURIComponent(url.username),
      password: decodeURIComponent(url.password),
      ...(sslmode && (sslModes as readonly string[]).includes(sslmode) ? { ssl_mode: sslmode } : {}),
    }
  } catch {
    return null
  }
}
