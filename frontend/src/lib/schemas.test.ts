import { describe, expect, it } from "vitest"

import { databaseSchema, loginSchema, parseConnectionString, registerSchema } from "./schemas"

describe("parseConnectionString", () => {
  it("parses a full URL with encoded credentials", () => {
    expect(parseConnectionString("postgres://app:s3cr%40t%20pw@db.example.com:6543/shop?sslmode=require")).toEqual({
      engine: "postgres",
      host: "db.example.com",
      port: 6543,
      database: "shop",
      username: "app",
      password: "s3cr@t pw",
      ssl_mode: "require",
    })
  })
  it("defaults the port and ignores unknown ssl modes", () => {
    const r = parseConnectionString("postgresql://u@localhost/db?sslmode=bogus")
    expect(r?.port).toBe(5432)
    expect(r && "ssl_mode" in r).toBe(false)
  })
  it("parses MySQL and MariaDB URLs", () => {
    expect(parseConnectionString("mysql://app:pw@db.example.com/shop?ssl-mode=REQUIRED")).toEqual({
      engine: "mysql",
      host: "db.example.com",
      port: 3306,
      database: "shop",
      username: "app",
      password: "pw",
      ssl_mode: "require",
    })
    expect(parseConnectionString("mariadb://u@h:3307/db")).toMatchObject({ engine: "mariadb", port: 3307 })
    // PostgreSQL-only modes don't apply to MySQL.
    expect(parseConnectionString("mysql://u@h/db?sslmode=verify-ca")).not.toHaveProperty("ssl_mode")
  })
  it("rejects unsupported URLs", () => {
    expect(parseConnectionString("sqlserver://u@h/db")).toBeNull()
    expect(parseConnectionString("not a url")).toBeNull()
  })
})

describe("databaseSchema", () => {
  const valid = { engine: "postgres", name: "production", host: "db.example.com", port: 5432, database: "app", username: "app", password: "pw", ssl_mode: "prefer" }
  it("accepts a valid database", () => {
    expect(databaseSchema(true).safeParse(valid).success).toBe(true)
  })
  it("requires a password only when creating", () => {
    expect(databaseSchema(true).safeParse({ ...valid, password: "" }).success).toBe(false)
    expect(databaseSchema(false).safeParse({ ...valid, password: "" }).success).toBe(true)
  })
  it("rejects hosts with schemes, ports or paths", () => {
    for (const host of ["postgres://db", "db:5432", "db/x", "a b"]) {
      expect(databaseSchema(true).safeParse({ ...valid, host }).success, host).toBe(false)
    }
  })
  it("accepts only the engine's SSL modes", () => {
    expect(databaseSchema(true).safeParse({ ...valid, engine: "mysql", port: 3306, ssl_mode: "require" }).success).toBe(true)
    expect(databaseSchema(true).safeParse({ ...valid, engine: "mysql", port: 3306, ssl_mode: "allow" }).success).toBe(false)
    expect(databaseSchema(true).safeParse({ ...valid, engine: "oracle" }).success).toBe(false)
  })
  it("rejects database names that look like options", () => {
    expect(databaseSchema(true).safeParse({ ...valid, database: "--help" }).success).toBe(false)
  })
  it("coerces and bounds the port", () => {
    expect(databaseSchema(true).safeParse({ ...valid, port: "6543" }).success).toBe(true)
    expect(databaseSchema(true).safeParse({ ...valid, port: 70000 }).success).toBe(false)
  })
})

describe("auth schemas", () => {
  it("validates login and registration", () => {
    expect(loginSchema.safeParse({ email: "a@b.co", password: "x" }).success).toBe(true)
    expect(loginSchema.safeParse({ email: "nope", password: "x" }).success).toBe(false)
    expect(registerSchema.safeParse({ name: "Ada", email: "a@b.co", password: "short" }).success).toBe(false)
    expect(registerSchema.safeParse({ name: "Ada", email: "a@b.co", password: "long-enough-pass" }).success).toBe(true)
  })
})

describe("host validation", () => {
  const base = { engine: "postgres", name: "p", port: 5432, database: "app", username: "app", password: "pw", ssl_mode: "prefer" }
  it.each(["localhost", "db.example.com", "10.0.0.5", "::1", "[::1]", "fe80::1", "2001:db8::8a2e:370:7334", "my_db-host.internal"])("accepts %s", (host) => {
    expect(databaseSchema(true).safeParse({ ...base, host }).success).toBe(true)
  })
})
