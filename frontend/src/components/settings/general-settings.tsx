"use client"

import { useState } from "react"
import { toast } from "sonner"

import { CopyField } from "@/components/app/copy-button"
import { PageHeader } from "@/components/app/page-header"
import { StatusBadge, StatusDot } from "@/components/app/status"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { errorMessage } from "@/lib/api"
import { useOrg } from "@/lib/org"
import { useOrganization, useSystemStatus, useUpdateOrganization, useUpdateProfile } from "@/lib/queries"

import { SettingsNav } from "./settings-nav"

export function GeneralSettings() {
  return (
    <div>
      <PageHeader title="Settings" description="Your profile, this organization and the DBVault installation." />
      <SettingsNav />
      <div className="grid gap-6 lg:grid-cols-2">
        <ProfileCard />
        <OrganizationCard />
        <div className="lg:col-span-2">
          <SystemCard />
        </div>
      </div>
    </div>
  )
}

function ProfileCard() {
  const { me } = useOrg()
  const [name, setName] = useState(me.user.name)
  const update = useUpdateProfile()
  return (
    <Card>
      <CardHeader>
        <CardTitle>Profile</CardTitle>
        <CardDescription>How you appear to teammates and in the audit log.</CardDescription>
      </CardHeader>
      <CardContent>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="profile-name">Name</FieldLabel>
            <Input id="profile-name" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field>
            <FieldLabel htmlFor="profile-email">Email</FieldLabel>
            <Input id="profile-email" value={me.user.email} readOnly disabled />
          </Field>
        </FieldGroup>
      </CardContent>
      <CardFooter className="justify-end border-t pt-4">
        <Button
          disabled={!name.trim() || name.trim() === me.user.name || update.isPending}
          onClick={() => update.mutate(name.trim(), { onSuccess: () => toast.success("Profile updated"), onError: (e) => toast.error(errorMessage(e)) })}
        >
          {update.isPending && <Spinner />} Save
        </Button>
      </CardFooter>
    </Card>
  )
}

function OrganizationCard() {
  const org = useOrganization()
  // Remount the form when the saved name changes so the input resets to it.
  return <OrganizationForm key={org.data?.name ?? "loading"} org={org.data} />
}

function OrganizationForm({ org: data }: { org: ReturnType<typeof useOrganization>["data"] }) {
  const { can } = useOrg()
  const org = { data }
  const [name, setName] = useState(data?.name ?? "")
  const update = useUpdateOrganization()
  return (
    <Card>
      <CardHeader>
        <CardTitle>Organization</CardTitle>
        <CardDescription>Databases, storage, backups and encryption keys belong to the organization.</CardDescription>
      </CardHeader>
      <CardContent>
        {!org.data ? (
          <Skeleton className="h-40" />
        ) : (
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="org-name">Name</FieldLabel>
              <Input id="org-name" value={name} maxLength={80} disabled={!can("admin")} onChange={(e) => setName(e.target.value)} />
            </Field>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <div className="text-sm font-medium">Slug</div>
                <CopyField value={org.data.slug} />
              </div>
              <div className="space-y-1.5">
                <div className="text-sm font-medium">ID</div>
                <CopyField value={org.data.id} />
              </div>
            </div>
            <div className="flex gap-6 text-sm">
              <div>
                <div className="text-xs text-muted-foreground">Your role</div>
                <div className="font-medium capitalize">{org.data.role}</div>
              </div>
              <div>
                <div className="text-xs text-muted-foreground">Members</div>
                <div className="font-medium tabular">{org.data.member_count}</div>
              </div>
            </div>
          </FieldGroup>
        )}
      </CardContent>
      {can("admin") && (
        <CardFooter className="justify-end border-t pt-4">
          <Button
            disabled={!org.data || !name.trim() || name.trim() === org.data.name || update.isPending}
            onClick={() => update.mutate(name.trim(), { onSuccess: () => toast.success("Organization renamed"), onError: (e) => toast.error(errorMessage(e)) })}
          >
            {update.isPending && <Spinner />} Save
          </Button>
        </CardFooter>
      )}
    </Card>
  )
}

function SystemCard() {
  const status = useSystemStatus()
  const s = status.data
  return (
    <Card>
      <CardHeader>
        <CardTitle>System</CardTitle>
        <CardDescription>What this DBVault installation can do right now.</CardDescription>
      </CardHeader>
      <CardContent>
        {!s ? (
          <Skeleton className="h-32" />
        ) : (
          <div className="space-y-5">
            <dl className="grid gap-4 text-sm sm:grid-cols-2 lg:grid-cols-4">
              <div>
                <dt className="text-xs text-muted-foreground">Version</dt>
                <dd className="font-mono">{s.version}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">Restore testing</dt>
                <dd className="space-y-1">
                  <StatusBadge status={s.verification.available ? "pass" : "unavailable"} label={s.verification.available ? `Available (${s.verification.mode})` : "Unavailable"} />
                  {s.verification.message && <p className="text-xs text-muted-foreground">{s.verification.message}</p>}
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">Email (SMTP)</dt>
                <dd>
                  <StatusBadge status={s.email_configured ? "pass" : "unavailable"} label={s.email_configured ? "Configured" : "Not configured"} />
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">Built-in storage</dt>
                <dd>
                  <StatusBadge status={s.builtin_storage ? "pass" : "none"} label={s.builtin_storage ? "Available" : "Not configured"} />
                </dd>
              </div>
            </dl>
            <div className="space-y-2">
              <div className="text-sm font-medium">Workers</div>
              {s.workers.length === 0 ? (
                <p className="flex items-center gap-2 text-sm text-destructive">
                  <StatusDot tone="error" /> No worker is running — backups and restores stay queued until one starts.
                </p>
              ) : (
                <ul className="divide-y rounded-lg border">
                  {s.workers.map((w) => (
                    <li key={w.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2 text-sm">
                      <StatusDot tone="success" />
                      <span className="font-mono text-xs">{w.hostname}</span>
                      <Badge variant="outline">pg_dump {w.capabilities.pg_dump_version ?? "missing"}</Badge>
                      {Object.entries(w.capabilities.engines ?? {}).map(([id, e]) => (
                        <Badge
                          key={id}
                          variant="outline"
                          title={e.verify_detail}
                          className={!e.tools_available ? "border-destructive/40 text-destructive" : undefined}
                        >
                          {e.label}
                          {!e.tools_available ? " · tools missing" : e.verify_available ? " · restore tests" : " · no restore tests"}
                        </Badge>
                      ))}
                      <span className="text-xs text-muted-foreground tabular">
                        {w.active_jobs}/{w.concurrency} jobs running
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
