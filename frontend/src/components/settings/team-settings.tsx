"use client"

import { zodResolver } from "@hookform/resolvers/zod"
import { useQueryClient } from "@tanstack/react-query"
import { MailPlus, Users } from "lucide-react"
import { useRouter } from "next/navigation"
import { useMemo, useState } from "react"
import { Controller, useForm } from "react-hook-form"
import { toast } from "sonner"
import { z } from "zod"

import { ConfirmDialog } from "@/components/app/confirm-dialog"
import { ALL, ClearFiltersButton, FilterSelect, matches, TablePagination, TableSearch, TableToolbar, usePaging } from "@/components/app/data-table"
import { CopyField } from "@/components/app/copy-button"
import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { FormField } from "@/components/app/form-field"
import { PageHeader, SectionHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { FieldGroup } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { errorMessage } from "@/lib/api"
import { useOrg } from "@/lib/org"
import { useChangeRole, useInvitations, useInvite, useMembers, useRemoveMember, useRevokeInvitation } from "@/lib/queries"
import type { Invitation, Member, Role } from "@/lib/types"

import { SettingsNav } from "./settings-nav"

const ROLES: { role: Role; description: string }[] = [
  { role: "owner", description: "Everything, plus managing roles, exporting the recovery key and deleting the organization." },
  { role: "admin", description: "Manage databases, storage, schedules and notifications; restore; delete backups; invite members." },
  { role: "member", description: "Run backups, verify backups, test connections and download encrypted artifacts." },
  { role: "viewer", description: "Read-only access to dashboards, backups and logs. Never sees secrets." },
]

export function TeamSettings() {
  const { can, me } = useOrg()
  const members = useMembers()
  const [memberSearch, setMemberSearch] = useState("")
  const [roleFilter, setRoleFilter] = useState(ALL)
  const memberRows = useMemo(
    () => (members.data ?? []).filter((m) => matches(memberSearch, m.name, m.email) && (roleFilter === ALL || m.role === roleFilter)),
    [members.data, memberSearch, roleFilter],
  )
  const memberPaging = usePaging(memberRows, JSON.stringify({ memberSearch, roleFilter }))
  const invitations = useInvitations(can("admin"))
  const [inviting, setInviting] = useState(false)
  const [removing, setRemoving] = useState<Member>()
  const [revoking, setRevoking] = useState<Invitation>()
  const remove = useRemoveMember()
  const revoke = useRevokeInvitation()
  const self = removing?.user_id === me.user.id
  const qc = useQueryClient()
  const router = useRouter()

  return (
    <div>
      <PageHeader
        title="Team"
        description="People with access to this organization and what they can do."
        actions={
          can("admin") && (
            <Button onClick={() => setInviting(true)}>
              <MailPlus /> Invite member
            </Button>
          )
        }
      />
      <SettingsNav />
      <div className="space-y-8">
        {!members.isPending && (members.data?.length ?? 0) > 0 && (
          <TableToolbar>
            <TableSearch value={memberSearch} onChange={setMemberSearch} placeholder="Search name or email…" />
            <FilterSelect
              value={roleFilter}
              onChange={setRoleFilter}
              label="Filter by role"
              allLabel="All roles"
              options={["owner", "admin", "member", "viewer"].map((r) => ({ value: r, label: r.charAt(0).toUpperCase() + r.slice(1) }))}
            />
            <ClearFiltersButton
              show={memberSearch.trim() !== "" || roleFilter !== ALL}
              onClear={() => {
                setMemberSearch("")
                setRoleFilter(ALL)
              }}
            />
          </TableToolbar>
        )}
        {members.error && <ErrorState error={members.error} retry={() => members.refetch()} />}
        {members.isPending ? (
          <TableSkeleton rows={3} columns={5} />
        ) : memberRows.length === 0 ? (
          <EmptyState icon={Users} title="No members match these filters" description="Try a different search or role." className="py-10" />
        ) : (
          <div className="space-y-3">
            <div className="overflow-hidden rounded-xl border bg-card">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="pl-4">Member</TableHead>
                    <TableHead>Role</TableHead>
                    <TableHead className="hidden md:table-cell">Joined</TableHead>
                    <TableHead className="hidden md:table-cell">Last login</TableHead>
                    <TableHead className="pr-4 text-right" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {memberPaging.rows.map((m) => (
                    <MemberRow key={m.user_id} m={m} onRemove={() => setRemoving(m)} />
                  ))}
                </TableBody>
              </Table>
            </div>
            <TablePagination {...memberPaging.props} noun="members" />
          </div>
        )}

        {can("admin") && (
          <section>
            <SectionHeader title="Pending invitations" description="Invitations expire after 7 days." />
            {invitations.isPending ? (
              <TableSkeleton rows={2} columns={4} />
            ) : invitations.data?.length === 0 ? (
              <EmptyState
                icon={Users}
                title="No pending invitations"
                description="Invite teammates to share responsibility for your backups."
                className="py-10"
              />
            ) : (
              <div className="overflow-hidden rounded-xl border bg-card">
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="pl-4">Email</TableHead>
                      <TableHead>Role</TableHead>
                      <TableHead className="hidden sm:table-cell">Expires</TableHead>
                      <TableHead className="pr-4 text-right" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {invitations.data?.map((i) => (
                      <TableRow key={i.id}>
                        <TableCell className="pl-4">{i.email}</TableCell>
                        <TableCell className="capitalize">{i.role}</TableCell>
                        <TableCell className="hidden text-muted-foreground sm:table-cell">
                          <RelativeTime date={i.expires_at} />
                        </TableCell>
                        <TableCell className="pr-4 text-right">
                          <Button variant="ghost" size="sm" onClick={() => setRevoking(i)}>
                            Revoke
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </section>
        )}

        <section>
          <SectionHeader title="Roles" description="Permissions are enforced by the API on every request." />
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {ROLES.map((r) => (
              <Card key={r.role} className="gap-1.5 px-4 py-4">
                <div className="font-medium capitalize">{r.role}</div>
                <p className="text-sm text-muted-foreground">{r.description}</p>
              </Card>
            ))}
          </div>
        </section>
      </div>

      <InviteDialog open={inviting} onOpenChange={setInviting} />
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(undefined)}
        title={self ? "Leave this organization?" : `Remove ${removing?.name}?`}
        description={<p>{self ? "You'll lose access to its databases and backups." : `${removing?.email} loses access immediately.`}</p>}
        confirmLabel={self ? "Leave organization" : "Remove member"}
        pending={remove.isPending}
        onConfirm={() =>
          removing &&
          remove.mutate(removing.user_id, {
            onSuccess: () => {
              setRemoving(undefined)
              if (self) {
                toast.success("You left the organization")
                void qc.resetQueries()
                router.push("/dashboard")
              } else toast.success("Member removed")
            },
            onError: (e) => toast.error(errorMessage(e)),
          })
        }
      />
      <ConfirmDialog
        open={!!revoking}
        onOpenChange={(o) => !o && setRevoking(undefined)}
        title="Revoke invitation?"
        description={<p>The invitation link for {revoking?.email} stops working.</p>}
        confirmLabel="Revoke"
        pending={revoke.isPending}
        onConfirm={() =>
          revoking &&
          revoke.mutate(revoking.id, {
            onSuccess: () => {
              toast.success("Invitation revoked")
              setRevoking(undefined)
            },
            onError: (e) => toast.error(errorMessage(e)),
          })
        }
      />
    </div>
  )
}

function MemberRow({ m, onRemove }: { m: Member; onRemove: () => void }) {
  const { can, me, org } = useOrg()
  const change = useChangeRole()
  const isSelf = m.user_id === me.user.id
  const isOwner = org.role === "owner"
  // Admins may change member/viewer roles; only owners touch owner/admin.
  const editable = !isSelf && can("admin") && (isOwner || (m.role !== "owner" && m.role !== "admin"))
  const choices: Role[] = isOwner ? ["owner", "admin", "member", "viewer"] : ["member", "viewer"]
  const canRemove = isSelf || (can("admin") && (isOwner || (m.role !== "owner" && m.role !== "admin")))
  return (
    <TableRow>
      <TableCell className="pl-4">
        <div className="flex items-center gap-2 font-medium">
          {m.name}
          {isSelf && <Badge variant="secondary">You</Badge>}
        </div>
        <div className="text-xs text-muted-foreground">{m.email}</div>
      </TableCell>
      <TableCell>
        {editable ? (
          <Select
            value={m.role}
            disabled={change.isPending}
            onValueChange={(role) =>
              change.mutate(
                { userId: m.user_id, role: role as Role },
                {
                  onSuccess: () => toast.success(`${m.name} is now ${role}`),
                  onError: (e) => toast.error("Couldn't change role", { description: errorMessage(e) }),
                },
              )
            }
          >
            <SelectTrigger size="sm" className="w-28 capitalize">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {choices.map((r) => (
                <SelectItem key={r} value={r} className="capitalize">
                  {r}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <span className="capitalize">{m.role}</span>
        )}
      </TableCell>
      <TableCell className="hidden text-muted-foreground md:table-cell">
        <RelativeTime date={m.joined_at} />
      </TableCell>
      <TableCell className="hidden text-muted-foreground md:table-cell">{m.last_login_at ? <RelativeTime date={m.last_login_at} /> : "Never"}</TableCell>
      <TableCell className="pr-4 text-right">
        {canRemove && (
          <Button variant="ghost" size="sm" onClick={onRemove}>
            {isSelf ? "Leave" : "Remove"}
          </Button>
        )}
      </TableCell>
    </TableRow>
  )
}

const inviteSchema = z.object({ email: z.email("Enter a valid email address."), role: z.enum(["admin", "member", "viewer"]) })
type InviteValues = z.infer<typeof inviteSchema>

function InviteDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { org } = useOrg()
  const invite = useInvite()
  const [result, setResult] = useState<{ url: string; emailSent: boolean } | null>(null)
  const form = useForm<InviteValues>({ resolver: zodResolver(inviteSchema), defaultValues: { email: "", role: "member" } })
  const close = (o: boolean) => {
    onOpenChange(o)
    if (!o) {
      setResult(null)
      form.reset()
    }
  }
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent>
        {result ? (
          <>
            <DialogHeader>
              <DialogTitle>Invitation created</DialogTitle>
              <DialogDescription>
                {result.emailSent
                  ? "We emailed the invitation. You can also share this link directly:"
                  : "Email isn't configured on this server — share this link with your teammate:"}
              </DialogDescription>
            </DialogHeader>
            <CopyField value={result.url} />
            <Alert>
              <AlertDescription>The link is shown once and expires in 7 days. It only works for the invited email address.</AlertDescription>
            </Alert>
            <DialogFooter>
              <Button onClick={() => close(false)}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <form
            noValidate
            className="space-y-5"
            onSubmit={form.handleSubmit((v) =>
              invite.mutate(v, {
                onSuccess: (r) => setResult({ url: r.invitation.url ?? "", emailSent: r.email_sent }),
                onError: (e) => toast.error("Couldn't invite", { description: errorMessage(e) }),
              }),
            )}
          >
            <DialogHeader>
              <DialogTitle>Invite to {org.name}</DialogTitle>
              <DialogDescription>They&apos;ll join with the role you choose. You can change it later.</DialogDescription>
            </DialogHeader>
            <FieldGroup>
              <FormField id="inv-email" label="Email" error={form.formState.errors.email}>
                <Input id="inv-email" type="email" autoFocus {...form.register("email")} />
              </FormField>
              <FormField id="inv-role" label="Role">
                <Controller
                  control={form.control}
                  name="role"
                  render={({ field }) => (
                    <Select value={field.value} onValueChange={field.onChange}>
                      <SelectTrigger id="inv-role" className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {org.role === "owner" && <SelectItem value="admin">Admin</SelectItem>}
                        <SelectItem value="member">Member</SelectItem>
                        <SelectItem value="viewer">Viewer</SelectItem>
                      </SelectContent>
                    </Select>
                  )}
                />
              </FormField>
            </FieldGroup>
            <DialogFooter>
              <Button type="submit" disabled={invite.isPending}>
                {invite.isPending && <Spinner />} Send invitation
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}
