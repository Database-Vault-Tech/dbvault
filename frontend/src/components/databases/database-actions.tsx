"use client"

import { Eye, MoreHorizontal, Pencil, Play, PlugZap, Trash2 } from "lucide-react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { useState } from "react"
import { toast } from "sonner"

import { ConfirmDialog } from "@/components/app/confirm-dialog"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { errorMessage } from "@/lib/api"
import { useOrg } from "@/lib/org"
import { useCreateBackup, useDeleteDatabase, useTestSavedDatabase } from "@/lib/queries"
import type { Database } from "@/lib/types"

export function useTestDatabaseToast() {
  const test = useTestSavedDatabase()
  const run = (db: Pick<Database, "id" | "name">) => {
    const id = toast.loading(`Testing connection to ${db.name}…`)
    test.mutate(db.id, {
      onSuccess: (r) =>
        r.ok
          ? toast.success("Connection successful", { id, description: r.server ? `PostgreSQL ${r.server.version} · ${r.server.latency_ms} ms` : undefined })
          : toast.error("Connection failed", { id, description: r.message }),
      onError: (err) => toast.error("Connection test failed", { id, description: errorMessage(err) }),
    })
  }
  return { run, pending: test.isPending }
}

export function DeleteDatabaseDialog({
  db,
  open,
  onOpenChange,
  onDeleted,
}: {
  db: Pick<Database, "id" | "name">
  open: boolean
  onOpenChange: (o: boolean) => void
  onDeleted?: () => void
}) {
  const del = useDeleteDatabase()
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Delete ${db.name}?`}
      confirmLabel="Delete database"
      phrase={db.name}
      pending={del.isPending}
      description={
        <>
          <p>DBVault will stop backing up this database and remove its schedules. Nothing in the PostgreSQL server itself is touched.</p>
          <p>Existing backups are kept: they stay listed, downloadable and restorable until you delete them.</p>
        </>
      }
      onConfirm={() =>
        del.mutate(db.id, {
          onSuccess: () => {
            toast.success(`${db.name} removed`)
            onOpenChange(false)
            onDeleted?.()
          },
          onError: (err) => toast.error("Couldn't delete database", { description: errorMessage(err) }),
        })
      }
    />
  )
}

export function DatabaseRowActions({ db }: { db: Database }) {
  const { can } = useOrg()
  const router = useRouter()
  const [deleting, setDeleting] = useState(false)
  const test = useTestDatabaseToast()
  const backup = useCreateBackup()
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label={`Actions for ${db.name}`} onClick={(e) => e.stopPropagation()}>
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48" onClick={(e) => e.stopPropagation()}>
          <DropdownMenuItem asChild>
            <Link href={`/databases/${db.id}`}>
              <Eye /> View
            </Link>
          </DropdownMenuItem>
          {can("member") && (
            <>
              <DropdownMenuItem onSelect={() => test.run(db)}>
                <PlugZap /> Test connection
              </DropdownMenuItem>
              <DropdownMenuItem
                onSelect={() =>
                  backup.mutate(
                    { database_id: db.id },
                    {
                      onSuccess: (q) => {
                        toast.success(`Backup of ${db.name} queued`)
                        router.push(`/backups/${q.backup_id}`)
                      },
                      onError: (err) => toast.error("Couldn't start backup", { description: errorMessage(err) }),
                    },
                  )
                }
              >
                <Play /> Run backup
              </DropdownMenuItem>
            </>
          )}
          {can("admin") && (
            <>
              <DropdownMenuItem asChild>
                <Link href={`/databases/${db.id}?edit=1`}>
                  <Pencil /> Edit
                </Link>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onSelect={() => setDeleting(true)}>
                <Trash2 /> Delete
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      <DeleteDatabaseDialog db={db} open={deleting} onOpenChange={setDeleting} />
    </>
  )
}
