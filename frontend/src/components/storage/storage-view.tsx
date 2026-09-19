"use client"

import { Cloud, HardDrive, MoreHorizontal, Pencil, Plus, Server, Star, Trash2, Zap } from "lucide-react"
import { useRouter, useSearchParams } from "next/navigation"
import { useState } from "react"
import { toast } from "sonner"

import { ConfirmDialog } from "@/components/app/confirm-dialog"
import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { PageHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { StatusBadge } from "@/components/app/status"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { errorMessage } from "@/lib/api"
import { formatBytes, formatNumber, storageLabels } from "@/lib/format"
import { useOrg } from "@/lib/org"
import { useDeleteStorage, useStorage, useTestStorage, useUpdateStorage } from "@/lib/queries"
import type { StorageDestination } from "@/lib/types"

import { StorageDialog } from "./storage-form"

const ICONS = { local: HardDrive, s3: Cloud, r2: Cloud, minio: Server } as const

export function StorageView() {
  const params = useSearchParams()
  const router = useRouter()
  const { can } = useOrg()
  const storage = useStorage()
  const [creating, setCreating] = useState(params.get("new") === "1")
  const [editing, setEditing] = useState<StorageDestination | undefined>()
  const [deleting, setDeleting] = useState<StorageDestination | undefined>()
  const del = useDeleteStorage()

  const closeCreate = (o: boolean) => {
    setCreating(o)
    if (!o && params.get("new")) router.replace("/storage")
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Storage"
        description="Destinations where encrypted backups are uploaded. Use S3, Cloudflare R2, MinIO or a local directory."
        actions={
          can("admin") && (
            <Button onClick={() => setCreating(true)}>
              <Plus /> Add storage
            </Button>
          )
        }
      />
      {storage.error && <ErrorState error={storage.error} retry={() => storage.refetch()} />}
      {storage.isPending ? (
        <div className="grid gap-4 lg:grid-cols-2">
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className="h-44 rounded-xl" />
          ))}
        </div>
      ) : storage.data?.length === 0 ? (
        <EmptyState
          icon={HardDrive}
          title="No storage destinations yet"
          description="Backups need somewhere to live. Connect an S3 bucket, Cloudflare R2, MinIO, or a local directory."
          action={
            can("admin") && (
              <Button onClick={() => setCreating(true)}>
                <Plus /> Add storage
              </Button>
            )
          }
        />
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {storage.data?.map((d) => (
            <DestinationCard key={d.id} d={d} onEdit={() => setEditing(d)} onDelete={() => setDeleting(d)} />
          ))}
        </div>
      )}

      <StorageDialog open={creating} onOpenChange={closeCreate} />
      <StorageDialog open={!!editing} onOpenChange={(o) => !o && setEditing(undefined)} destination={editing} />
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(undefined)}
        title={`Delete ${deleting?.name}?`}
        description={
          <p>
            DBVault only deletes destinations that no schedule uses and that hold no backups, so nothing is orphaned. Files already in the bucket are not
            touched.
          </p>
        }
        confirmLabel="Delete destination"
        pending={del.isPending}
        onConfirm={() =>
          deleting &&
          del.mutate(deleting.id, {
            onSuccess: () => {
              toast.success(`Deleted ${deleting.name}`)
              setDeleting(undefined)
            },
            onError: (e) => toast.error("Couldn't delete destination", { description: errorMessage(e) }),
          })
        }
      />
    </div>
  )
}

function DestinationCard({ d, onEdit, onDelete }: { d: StorageDestination; onEdit: () => void; onDelete: () => void }) {
  const { can } = useOrg()
  const test = useTestStorage()
  const update = useUpdateStorage(d.id)
  const Icon = ICONS[d.type]
  const setDefault = () =>
    update.mutate(
      { name: d.name, type: d.type, config: d.config, is_default: true },
      { onSuccess: () => toast.success(`${d.name} is now the default`), onError: (e) => toast.error(errorMessage(e)) },
    )
  return (
    <Card className="gap-4 px-5 py-5">
      <div className="flex items-start gap-3">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted/50">
          <Icon className="size-4 text-muted-foreground" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="truncate font-medium">{d.name}</h3>
            {d.is_default && (
              <Badge variant="secondary" className="gap-1">
                <Star className="fill-current" /> Default
              </Badge>
            )}
          </div>
          <div className="text-xs text-muted-foreground">{storageLabels[d.type]}</div>
        </div>
        <div className="flex items-center gap-1">
          {can("member") && (
            <Button
              variant="outline"
              size="sm"
              disabled={test.isPending}
              onClick={() =>
                test.mutate(d.id, {
                  onSuccess: (r) => (r.ok ? toast.success(`${d.name}: connection OK`, { description: r.message }) : toast.error(`${d.name}: test failed`, { description: r.message })),
                  onError: (e) => toast.error(errorMessage(e)),
                })
              }
            >
              {test.isPending ? <Spinner /> : <Zap />} Test
            </Button>
          )}
          {can("admin") && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon-sm" aria-label="Destination actions">
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={onEdit}>
                  <Pencil /> Edit
                </DropdownMenuItem>
                {!d.is_default && (
                  <DropdownMenuItem onSelect={setDefault}>
                    <Star /> Set as default
                  </DropdownMenuItem>
                )}
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onSelect={onDelete}>
                  <Trash2 /> Delete
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
      </div>
      <code className="truncate rounded-md border bg-muted/40 px-2.5 py-1.5 font-mono text-xs">{d.location}</code>
      <dl className="grid grid-cols-3 gap-3 text-sm">
        <div>
          <dt className="text-xs text-muted-foreground">Backups</dt>
          <dd className="font-medium tabular">{formatNumber(d.backup_count)}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Used</dt>
          <dd className="font-medium tabular">{formatBytes(d.used_bytes)}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Schedules</dt>
          <dd className="font-medium tabular">{d.schedule_count}</dd>
        </div>
      </dl>
      <div className="flex flex-wrap items-center gap-2 border-t pt-3 text-xs text-muted-foreground">
        {d.last_test_ok === null ? (
          <StatusBadge status="none" label="Not tested" />
        ) : (
          <StatusBadge status={d.last_test_ok ? "pass" : "fail"} label={d.last_test_ok ? "Reachable" : "Test failed"} />
        )}
        {d.last_tested_at && <RelativeTime date={d.last_tested_at} />}
        {d.last_test_error && <span className="w-full text-destructive">{d.last_test_error}</span>}
        {d.access_key_hint && <span className="ml-auto font-mono">key {d.access_key_hint}</span>}
      </div>
    </Card>
  )
}
