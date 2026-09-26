"use client"

import { ArrowLeft, ShieldCheck } from "lucide-react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { useState } from "react"
import { toast } from "sonner"

import { PageHeader } from "@/components/app/page-header"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ENGINES, type SupportedEngine } from "@/lib/engines"
import { useOrg } from "@/lib/org"
import { useCreateDatabase } from "@/lib/queries"

import { DatabaseForm } from "./database-form"

const GRANTS: Record<SupportedEngine, { title?: string; intro: string; sql: string; note?: string }> = {
  postgres: {
    intro: "Backups only need read access. On PostgreSQL 14+ create a dedicated role:",
    sql: `CREATE ROLE dbvault LOGIN
  PASSWORD '…';
GRANT pg_read_all_data
  TO dbvault;`,
  },
  mysql: {
    intro: "Backups only need read access to the database. Create a dedicated user:",
    sql: `CREATE USER 'dbvault'@'%'
  IDENTIFIED BY '…';
GRANT SELECT, SHOW VIEW,
  TRIGGER, EVENT, LOCK TABLES
  ON app.* TO 'dbvault'@'%';`,
    note: "To back up stored procedures owned by other users, also grant SHOW_ROUTINE (MySQL 8.0.20+).",
  },
  mariadb: {
    intro: "Backups only need read access to the database. Create a dedicated user:",
    sql: `CREATE USER 'dbvault'@'%'
  IDENTIFIED BY '…';
GRANT SELECT, SHOW VIEW,
  TRIGGER, EVENT, LOCK TABLES
  ON app.* TO 'dbvault'@'%';`,
  },
  sqlite: {
    title: "Mount the SQLite folder",
    intro: "DBVault reads SQLite files from a folder mounted into the api and worker containers. In .env:",
    sql: `SQLITE_HOST_DIR=/srv/myapp/data`,
    note: "Then run docker compose up -d. Files appear under that folder, e.g. app.db. The containers run as uid 10001, which needs read and write access.",
  },
}

export function NewDatabaseView() {
  const router = useRouter()
  const { can } = useOrg()
  const create = useCreateDatabase()
  const [engine, setEngine] = useState<SupportedEngine>("postgres")
  const grants = GRANTS[engine]

  return (
    <div className="mx-auto max-w-5xl">
      <Button variant="ghost" size="sm" asChild className="mb-4 -ml-2 text-muted-foreground">
        <Link href="/databases">
          <ArrowLeft /> Databases
        </Link>
      </Button>
      <PageHeader title="Add database" description="Connect a PostgreSQL, MySQL, MariaDB or SQLite database. Test the connection before saving to catch firewall, credential or permission issues early." />
      {!can("admin") ? (
        <Alert>
          <AlertTitle>Admins only</AlertTitle>
          <AlertDescription>Ask an organization admin or owner to add databases.</AlertDescription>
        </Alert>
      ) : (
        <div className="grid gap-6 lg:grid-cols-[1fr_300px]">
          <Card>
            <CardContent>
              <DatabaseForm
                mode="create"
                submitLabel="Save database"
                onEngineChange={setEngine}
                onCancel={() => router.push("/databases")}
                onSubmit={async (input) => {
                  const res = await create.mutateAsync(input)
                  if (res.test.ok) toast.success(`${res.database.name} added`, { description: "Connection verified." })
                  else toast.warning(`${res.database.name} added, but the connection failed`, { description: res.test.message })
                  router.push(`/databases/${res.database.id}`)
                }}
              />
            </CardContent>
          </Card>
          <div className="space-y-4">
            <Card size="sm">
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-sm">
                  <ShieldCheck className="size-4 text-brand" /> {grants.title ?? "Least privilege"}
                </CardTitle>
                <CardDescription>{grants.intro}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-2">
                <pre className="overflow-x-auto rounded-md bg-muted p-3 font-mono text-[11.5px] leading-5">{grants.sql}</pre>
                {grants.note && <p className="text-xs text-muted-foreground">{grants.note}</p>}
              </CardContent>
            </Card>
            <Card size="sm">
              <CardHeader>
                <CardTitle className="text-sm">What happens next</CardTitle>
                <CardDescription>
                  Add a storage destination and a schedule, and DBVault will run <code className="font-mono">{ENGINES[engine].dumpTool}</code>, compress, encrypt, checksum and
                  upload every backup automatically.
                </CardDescription>
              </CardHeader>
            </Card>
          </div>
        </div>
      )}
    </div>
  )
}
