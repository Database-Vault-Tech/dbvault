"use client"

import { AlertOctagon, Archive, ArrowLeft, Database, Eye, HardDrive, Users } from "lucide-react"
import Link from "next/link"
import type { ReactNode } from "react"

import { ErrorState } from "@/components/app/error-state"
import { PageHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { StatCard } from "@/components/app/stat-card"
import { StatusBadge, StatusDot } from "@/components/app/status"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { engineMeta } from "@/lib/engines"
import { formatBytes, formatDuration, humanizeAction, storageLabels } from "@/lib/format"
import { useAdminOrganization } from "@/lib/queries"

import { AdminShell } from "./admin-shell"

function Section({ title, description, count, children }: { title: string; description?: string; count: number; children: ReactNode }) {
  return (
    <Card className="gap-0 overflow-hidden py-0">
      <CardHeader className="border-b py-4">
        <CardTitle className="flex items-center gap-2">
          {title} <span className="text-sm font-normal text-muted-foreground tabular">{count}</span>
        </CardTitle>
        {description && <CardDescription>{description}</CardDescription>}
      </CardHeader>
      <CardContent className="px-0">
        {count === 0 ? <p className="px-6 py-6 text-sm text-muted-foreground">None.</p> : <Table>{children}</Table>}
      </CardContent>
    </Card>
  )
}

function TestState({ ok }: { ok: boolean | null }) {
  const label = ok === null ? "Not tested" : ok ? "Reachable" : "Unreachable"
  return (
    <span className="inline-flex items-center gap-1.5 text-sm text-muted-foreground">
      <StatusDot tone={ok === null ? "neutral" : ok ? "success" : "error"} /> {label}
    </span>
  )
}

export function AdminOrganization({ id }: { id: string }) {
  const { data, isPending, error, refetch } = useAdminOrganization(id)
  const org = data?.organization

  const header = (
    <>
      <Button variant="ghost" size="sm" className="-ml-2 mb-2 text-muted-foreground" asChild>
        <Link href="/admin">
          <ArrowLeft /> All organizations
        </Link>
      </Button>
      <PageHeader
        eyebrow="Instance admin"
        title={org?.name ?? "Organization"}
        description={org ? `${org.slug} · owned by ${org.owner_email ?? "nobody"}` : undefined}
        actions={
          <span className="inline-flex items-center gap-1.5 rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground">
            <Eye className="size-3.5" /> Read-only · your visit is in their audit log
          </span>
        }
      />
    </>
  )

  if (error) {
    return (
      <AdminShell header={header}>
        <ErrorState error={error} retry={() => refetch()} />
      </AdminShell>
    )
  }
  if (isPending || !data || !org) {
    return (
      <AdminShell header={header}>
        <TableSkeleton rows={6} columns={4} />
      </AdminShell>
    )
  }

  return (
    <AdminShell header={header}>
      <div className="space-y-6">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <StatCard label="Members" icon={Users} value={org.members} />
          <StatCard label="Databases" icon={Database} value={org.databases} />
          <StatCard label="Backups" icon={Archive} value={org.backups} hint={org.last_backup_at ? <>Last <RelativeTime date={org.last_backup_at} /></> : "None yet"} />
          <StatCard label="Storage" icon={HardDrive} value={formatBytes(org.storage_bytes)} />
          <StatCard label="Failed" icon={AlertOctagon} tone={org.failed_backups_7d > 0 ? "error" : "default"} value={org.failed_backups_7d} hint="Last 7 days" />
        </div>

        <Section title="Members" count={data.members.length}>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-6">Name</TableHead>
              <TableHead>Role</TableHead>
              <TableHead className="hidden md:table-cell">Last sign-in</TableHead>
              <TableHead className="hidden pr-6 md:table-cell">Joined</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.members.map((m) => (
              <TableRow key={m.user_id}>
                <TableCell className="pl-6">
                  <div className="font-medium">{m.name}</div>
                  <div className="text-xs text-muted-foreground">{m.email}</div>
                </TableCell>
                <TableCell>
                  <Badge variant="outline" className="capitalize">
                    {m.role}
                  </Badge>
                </TableCell>
                <TableCell className="hidden text-muted-foreground md:table-cell">{m.last_login_at ? <RelativeTime date={m.last_login_at} /> : "Never"}</TableCell>
                <TableCell className="hidden pr-6 text-muted-foreground md:table-cell">
                  <RelativeTime date={m.joined_at} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Section>

        <Section title="Databases" description="Connection credentials are never shown here." count={data.databases.length}>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-6">Database</TableHead>
              <TableHead className="hidden md:table-cell">Last backup</TableHead>
              <TableHead className="hidden text-right sm:table-cell">Size</TableHead>
              <TableHead className="hidden pr-6 lg:table-cell">Connection</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.databases.map((d) => (
              <TableRow key={d.id}>
                <TableCell className="pl-6">
                  <div className="font-medium">{d.name}</div>
                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span className="max-w-64 truncate font-mono">
                      {d.host}:{d.port}/{d.database}
                    </span>
                    <span className="shrink-0 rounded-full bg-muted px-1.5 py-px font-mono text-[10.5px]">
                      {engineMeta(d.engine).shortLabel}
                      {d.version && ` ${d.version}`}
                    </span>
                  </div>
                </TableCell>
                <TableCell className="hidden md:table-cell">
                  {d.last_backup_status ? (
                    <span className="inline-flex items-center gap-2 text-sm text-muted-foreground">
                      <StatusDot status={d.last_backup_status} />
                      <RelativeTime date={d.last_backup_at} />
                    </span>
                  ) : (
                    <span className="text-sm text-muted-foreground">Never</span>
                  )}
                </TableCell>
                <TableCell className="hidden text-right tabular sm:table-cell">{formatBytes(d.size_bytes)}</TableCell>
                <TableCell className="hidden pr-6 lg:table-cell">
                  <TestState ok={d.last_test_ok} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Section>

        <div className="grid gap-6 xl:grid-cols-2">
          <Section title="Storage" description="Endpoints, buckets and keys are never shown here." count={data.storage.length}>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-6">Destination</TableHead>
                <TableHead className="text-right">Used</TableHead>
                <TableHead className="hidden pr-6 sm:table-cell">Connection</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.storage.map((s) => (
                <TableRow key={s.id}>
                  <TableCell className="pl-6">
                    <div className="flex items-center gap-2 font-medium">
                      {s.name}
                      {s.is_default && <Badge variant="secondary">Default</Badge>}
                    </div>
                    <div className="text-xs text-muted-foreground">{storageLabels[s.type] ?? s.type}</div>
                  </TableCell>
                  <TableCell className="text-right tabular">{formatBytes(s.used_bytes)}</TableCell>
                  <TableCell className="hidden pr-6 sm:table-cell">
                    <TestState ok={s.last_test_ok} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Section>

          <Section title="Schedules" count={data.schedules.length}>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-6">Schedule</TableHead>
                <TableHead className="pr-6">Next run</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.schedules.map((s) => (
                <TableRow key={s.id}>
                  <TableCell className="pl-6">
                    <div className="font-medium">
                      {s.database_name} <span className="font-normal text-muted-foreground">· {s.name}</span>
                    </div>
                    <div className="font-mono text-xs text-muted-foreground">
                      {s.cron_expression} · {s.timezone}
                    </div>
                  </TableCell>
                  <TableCell className="pr-6 text-muted-foreground">{s.enabled ? <RelativeTime date={s.next_run_at} /> : "Paused"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Section>
        </div>

        <Section title="Recent backups" description="The latest 50." count={data.backups.length}>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-6">Database</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="hidden text-right sm:table-cell">Size</TableHead>
              <TableHead className="hidden text-right md:table-cell">Duration</TableHead>
              <TableHead className="hidden pr-6 lg:table-cell">Started</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.backups.map((b) => (
              <TableRow key={b.id}>
                <TableCell className="pl-6">
                  <div className="font-medium">{b.database_name}</div>
                  <div className="text-xs text-muted-foreground capitalize">{b.trigger}</div>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap items-center gap-1.5">
                    <StatusBadge status={b.status} />
                    {b.verification_status !== "none" && <StatusBadge status={b.verification_status} />}
                  </div>
                  {b.error && <div className="mt-1 max-w-sm truncate text-xs text-destructive" title={b.error}>{b.error}</div>}
                </TableCell>
                <TableCell className="hidden text-right tabular sm:table-cell">{formatBytes(b.size_bytes)}</TableCell>
                <TableCell className="hidden text-right tabular text-muted-foreground md:table-cell">{formatDuration(b.duration_ms)}</TableCell>
                <TableCell className="hidden pr-6 text-muted-foreground lg:table-cell">
                  <RelativeTime date={b.created_at} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Section>

        <Section title="Audit log" description="The latest 50 entries." count={data.audit_logs.length}>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-6">Action</TableHead>
              <TableHead className="hidden md:table-cell">Actor</TableHead>
              <TableHead className="pr-6">When</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.audit_logs.map((l) => (
              <TableRow key={l.id}>
                <TableCell className="pl-6">{humanizeAction(l.action)}</TableCell>
                <TableCell className="hidden text-muted-foreground md:table-cell">{l.actor_email ?? (l.actor_type === "system" ? "System" : "—")}</TableCell>
                <TableCell className="pr-6 text-muted-foreground">
                  <RelativeTime date={l.created_at} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Section>
      </div>
    </AdminShell>
  )
}
