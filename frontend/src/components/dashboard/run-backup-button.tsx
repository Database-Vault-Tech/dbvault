"use client"

import { ChevronDown, Database, Play } from "lucide-react"
import { useRouter } from "next/navigation"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Spinner } from "@/components/ui/spinner"
import { errorMessage } from "@/lib/api"
import { useOrg } from "@/lib/org"
import { useCreateBackup, useDatabases } from "@/lib/queries"

/** Starts an on-demand backup and jumps to its live progress. */
export function RunBackupButton({ databaseId, variant = "default", size = "default" }: { databaseId?: string; variant?: "default" | "outline"; size?: "default" | "sm" }) {
  const router = useRouter()
  const { can } = useOrg()
  const { data: databases } = useDatabases()
  const create = useCreateBackup()

  const run = (id: string, name?: string) =>
    create.mutate(
      { database_id: id },
      {
        onSuccess: (q) => {
          toast.success(`Backup${name ? ` of ${name}` : ""} queued`, { description: "Following live progress…" })
          router.push(`/backups/${q.backup_id}`)
        },
        onError: (err) => toast.error("Couldn't start backup", { description: errorMessage(err) }),
      },
    )

  if (!can("member")) return null
  if (databaseId) {
    return (
      <Button variant={variant} size={size} disabled={create.isPending} onClick={() => run(databaseId)}>
        {create.isPending ? <Spinner /> : <Play />} Run backup
      </Button>
    )
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant={variant} size={size} disabled={create.isPending || !databases?.length}>
          {create.isPending ? <Spinner /> : <Play />} Run backup <ChevronDown className="opacity-60" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuLabel className="text-xs text-muted-foreground">Back up now</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {databases?.map((d) => (
          <DropdownMenuItem key={d.id} onSelect={() => run(d.id, d.name)}>
            <Database />
            <span className="truncate">{d.name}</span>
            <span className="ml-auto text-xs text-muted-foreground">{d.pg_version ? `PG ${d.pg_version.split(".")[0]}` : ""}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
