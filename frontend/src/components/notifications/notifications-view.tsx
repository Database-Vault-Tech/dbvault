"use client"

import { Bell, Mail, MoreHorizontal, Pencil, Plus, Send, Trash2, Webhook } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { ConfirmDialog } from "@/components/app/confirm-dialog"
import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { PageHeader, SectionHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { StatusBadge } from "@/components/app/status"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { errorMessage } from "@/lib/api"
import { useOrg } from "@/lib/org"
import { useDeleteNotification, useDeliveries, useNotifications, useTestNotification, useUpdateNotification } from "@/lib/queries"
import type { NotificationChannel } from "@/lib/types"

import { ChannelDialog } from "./channel-form"

export function NotificationsView() {
  const { can } = useOrg()
  const channels = useNotifications()
  const deliveries = useDeliveries()
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<NotificationChannel>()
  const [deleting, setDeleting] = useState<NotificationChannel>()
  const del = useDeleteNotification()

  return (
    <div className="space-y-8">
      <PageHeader
        title="Notifications"
        description="Email and webhook alerts for failed backups, failed verifications, storage problems and restores."
        actions={
          can("admin") && (
            <Button onClick={() => setCreating(true)}>
              <Plus /> Add channel
            </Button>
          )
        }
      />
      {channels.error && <ErrorState error={channels.error} retry={() => channels.refetch()} />}
      {channels.isPending ? (
        <TableSkeleton rows={3} columns={5} />
      ) : channels.data?.length === 0 ? (
        <EmptyState
          icon={Bell}
          title="No notification channels yet"
          description="Add an email or webhook channel so a failed backup never goes unnoticed."
          action={
            can("admin") && (
              <Button onClick={() => setCreating(true)}>
                <Plus /> Add channel
              </Button>
            )
          }
        />
      ) : (
        <div className="overflow-hidden rounded-xl border bg-card">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-4">Channel</TableHead>
                <TableHead>Destination</TableHead>
                <TableHead className="hidden md:table-cell">Events</TableHead>
                <TableHead className="hidden sm:table-cell">Last delivery</TableHead>
                <TableHead>Enabled</TableHead>
                <TableHead className="w-10 pr-4" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {channels.data?.map((c) => (
                <ChannelRow key={c.id} c={c} onEdit={() => setEditing(c)} onDelete={() => setDeleting(c)} />
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <section>
        <SectionHeader title="Recent deliveries" description="Every notification attempt, including retries. Failed deliveries are retried up to 4 times." />
        {deliveries.isPending ? (
          <TableSkeleton rows={3} columns={5} />
        ) : deliveries.error ? (
          <ErrorState error={deliveries.error} retry={() => deliveries.refetch()} />
        ) : deliveries.data?.length === 0 ? (
          <EmptyState icon={Send} title="No deliveries yet" description="Send a test from a channel to check it works end to end." className="py-10" />
        ) : (
          <div className="overflow-hidden rounded-xl border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="pl-4">Channel</TableHead>
                  <TableHead>Event</TableHead>
                  <TableHead className="hidden lg:table-cell">Title</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="hidden text-right sm:table-cell">Attempts</TableHead>
                  <TableHead className="pr-4 text-right">Time</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {deliveries.data?.map((d) => (
                  <TableRow key={d.id}>
                    <TableCell className="pl-4">
                      <span className="flex items-center gap-1.5">
                        {d.channel_type === "email" ? <Mail className="size-3.5 text-muted-foreground" /> : <Webhook className="size-3.5 text-muted-foreground" />}
                        {d.channel_name}
                      </span>
                    </TableCell>
                    <TableCell className="font-mono text-xs">{d.event}</TableCell>
                    <TableCell className="hidden max-w-xs truncate lg:table-cell">{d.title}</TableCell>
                    <TableCell>
                      <div className="flex flex-col items-start gap-1">
                        <StatusBadge status={d.status} />
                        {d.error && <span className="max-w-xs text-xs whitespace-normal text-destructive">{d.error}</span>}
                      </div>
                    </TableCell>
                    <TableCell className="hidden text-right tabular sm:table-cell">
                      {d.attempts}
                      {d.response_status ? <span className="text-muted-foreground"> · HTTP {d.response_status}</span> : null}
                    </TableCell>
                    <TableCell className="pr-4 text-right text-muted-foreground">
                      <RelativeTime date={d.created_at} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </section>

      <ChannelDialog open={creating} onOpenChange={setCreating} />
      <ChannelDialog open={!!editing} onOpenChange={(o) => !o && setEditing(undefined)} channel={editing} />
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(undefined)}
        title={`Delete ${deleting?.name}?`}
        description={<p>This channel stops receiving alerts. Its delivery history is removed.</p>}
        confirmLabel="Delete channel"
        pending={del.isPending}
        onConfirm={() =>
          deleting &&
          del.mutate(deleting.id, {
            onSuccess: () => {
              toast.success("Channel deleted")
              setDeleting(undefined)
            },
            onError: (e) => toast.error(errorMessage(e)),
          })
        }
      />
    </div>
  )
}

function ChannelRow({ c, onEdit, onDelete }: { c: NotificationChannel; onEdit: () => void; onDelete: () => void }) {
  const { can } = useOrg()
  const update = useUpdateNotification(c.id)
  const test = useTestNotification()
  return (
    <TableRow>
      <TableCell className="pl-4">
        <span className="flex items-center gap-2 font-medium">
          {c.type === "email" ? <Mail className="size-4 text-muted-foreground" /> : <Webhook className="size-4 text-muted-foreground" />}
          {c.name}
        </span>
      </TableCell>
      <TableCell className="max-w-56 truncate text-sm text-muted-foreground">
        {c.type === "email" ? c.config.recipients?.join(", ") : <span className="font-mono text-xs">{c.config.url_hint}</span>}
      </TableCell>
      <TableCell className="hidden md:table-cell">
        <div className="flex max-w-md flex-wrap gap-1">
          {c.events.map((e) => (
            <Badge key={e} variant="outline" className="font-mono text-[11px]">
              {e}
            </Badge>
          ))}
        </div>
      </TableCell>
      <TableCell className="hidden sm:table-cell">
        {c.last_delivery_status ? (
          <div className="flex flex-col items-start gap-1">
            <StatusBadge status={c.last_delivery_status} />
            <RelativeTime date={c.last_delivery_at} className="text-xs text-muted-foreground" />
          </div>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </TableCell>
      <TableCell>
        <Switch
          checked={c.enabled}
          disabled={!can("admin") || update.isPending}
          aria-label={c.enabled ? "Disable channel" : "Enable channel"}
          onCheckedChange={(enabled) =>
            update.mutate(
              { name: c.name, type: c.type, recipients: c.config.recipients, events: c.events, enabled },
              { onSuccess: () => toast.success(enabled ? "Channel enabled" : "Channel disabled"), onError: (e) => toast.error(errorMessage(e)) },
            )
          }
        />
      </TableCell>
      <TableCell className="pr-4">
        {can("admin") && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon-sm" aria-label="Channel actions">
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                onSelect={() =>
                  test.mutate(c.id, {
                    onSuccess: () => toast.success("Test queued", { description: "Check Recent deliveries for the result." }),
                    onError: (e) => toast.error(errorMessage(e)),
                  })
                }
              >
                <Send /> Send test
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={onEdit}>
                <Pencil /> Edit
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onSelect={onDelete}>
                <Trash2 /> Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </TableCell>
    </TableRow>
  )
}
