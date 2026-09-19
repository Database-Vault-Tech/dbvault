"use client"

import { ArrowLeft, ShieldCheck } from "lucide-react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { toast } from "sonner"

import { PageHeader } from "@/components/app/page-header"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useOrg } from "@/lib/org"
import { useCreateDatabase } from "@/lib/queries"

import { DatabaseForm } from "./database-form"

export function NewDatabaseView() {
  const router = useRouter()
  const { can } = useOrg()
  const create = useCreateDatabase()

  return (
    <div className="mx-auto max-w-5xl">
      <Button variant="ghost" size="sm" asChild className="mb-4 -ml-2 text-muted-foreground">
        <Link href="/databases">
          <ArrowLeft /> Databases
        </Link>
      </Button>
      <PageHeader title="Add database" description="Connect a PostgreSQL database. Test the connection before saving to catch firewall or credential issues early." />
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
                  <ShieldCheck className="size-4 text-brand" /> Least privilege
                </CardTitle>
                <CardDescription>
                  Backups only need read access. On PostgreSQL 14+ create a dedicated role:
                </CardDescription>
              </CardHeader>
              <CardContent>
                <pre className="overflow-x-auto rounded-md bg-muted p-3 font-mono text-[11.5px] leading-5">
{`CREATE ROLE dbvault LOGIN
  PASSWORD '…';
GRANT pg_read_all_data
  TO dbvault;`}
                </pre>
              </CardContent>
            </Card>
            <Card size="sm">
              <CardHeader>
                <CardTitle className="text-sm">What happens next</CardTitle>
                <CardDescription>
                  Add a storage destination and a schedule, and DBVault will run <code className="font-mono">pg_dump</code>, compress, encrypt, checksum and
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
