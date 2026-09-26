import type { DatabaseEngine, SSLMode } from "@/lib/types"

/** Engines DBVault can back up and restore end to end. Mirrors the backend drivers. */
export type SupportedEngine = Extract<DatabaseEngine, "postgres" | "mysql" | "mariadb" | "sqlite">

export interface EngineMeta {
  id: SupportedEngine
  label: string
  /** Compact label for badges, e.g. "PG". */
  shortLabel: string
  defaultPort: number
  defaultDatabase: string
  sslModes: readonly SSLMode[]
  /** A failed restore leaves the target unchanged. */
  atomicRestore: boolean
  /** A file in the folder mounted into DBVault (SQLITE_ROOT): no host, port or login. */
  fileBased: boolean
  dumpTool: string
  restoreTool: string
  /** Dump file extension before compression/encryption. */
  extension: string
  formatLabel: string
  urlSchemes: readonly string[]
  urlExample: string
}

export const ENGINES: Record<SupportedEngine, EngineMeta> = {
  postgres: {
    id: "postgres",
    label: "PostgreSQL",
    shortLabel: "PG",
    defaultPort: 5432,
    defaultDatabase: "postgres",
    sslModes: ["disable", "allow", "prefer", "require", "verify-ca", "verify-full"],
    atomicRestore: true,
    fileBased: false,
    dumpTool: "pg_dump",
    restoreTool: "pg_restore",
    extension: ".dump",
    formatLabel: "pg_dump custom",
    urlSchemes: ["postgres:", "postgresql:"],
    urlExample: "postgres://user:password@db.example.com:5432/app?sslmode=require",
  },
  mysql: {
    id: "mysql",
    label: "MySQL",
    shortLabel: "MySQL",
    defaultPort: 3306,
    defaultDatabase: "",
    sslModes: ["disable", "prefer", "require", "verify-full"],
    atomicRestore: false,
    fileBased: false,
    dumpTool: "mariadb-dump",
    restoreTool: "mariadb",
    extension: ".sql",
    formatLabel: "SQL dump",
    urlSchemes: ["mysql:"],
    urlExample: "mysql://user:password@db.example.com:3306/app",
  },
  mariadb: {
    id: "mariadb",
    label: "MariaDB",
    shortLabel: "MariaDB",
    defaultPort: 3306,
    defaultDatabase: "",
    sslModes: ["disable", "prefer", "require", "verify-full"],
    atomicRestore: false,
    fileBased: false,
    dumpTool: "mariadb-dump",
    restoreTool: "mariadb",
    extension: ".sql",
    formatLabel: "SQL dump",
    urlSchemes: ["mariadb:"],
    urlExample: "mariadb://user:password@db.example.com:3306/app",
  },
  sqlite: {
    id: "sqlite",
    label: "SQLite",
    shortLabel: "SQLite",
    defaultPort: 0,
    defaultDatabase: "",
    sslModes: ["disable"],
    atomicRestore: true,
    fileBased: true,
    dumpTool: "VACUUM INTO",
    restoreTool: "atomic file swap",
    extension: ".db",
    formatLabel: "SQLite file",
    urlSchemes: [],
    urlExample: "",
  },
}

export const ENGINE_LIST = Object.values(ENGINES)

/** Metadata for an engine id; unknown or missing ids fall back to PostgreSQL (rows created before engines existed). */
export function engineMeta(id: string | null | undefined): EngineMeta {
  return ENGINES[(id ?? "postgres") as SupportedEngine] ?? ENGINES.postgres
}

export function engineLabel(id: string | null | undefined): string {
  return engineMeta(id).label
}

/** Where a database lives, for lists and summaries: host:port/db, or the file path for SQLite. */
export function databaseLocation(d: { engine?: string | null; host: string; port: number; database: string }, withPort = true): string {
  if (engineMeta(d.engine).fileBased) return d.database
  return withPort ? `${d.host}:${d.port}/${d.database}` : `${d.host}/${d.database}`
}
