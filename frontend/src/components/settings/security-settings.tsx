"use client"

import { zodResolver } from "@hookform/resolvers/zod"
import { AlertTriangle, Download, KeyRound, Laptop, Plus } from "lucide-react"
import { type ReactNode, useMemo, useState } from "react"
import { useForm } from "react-hook-form"
import { toast } from "sonner"
import { z } from "zod"

import { ConfirmDialog } from "@/components/app/confirm-dialog"
import { matches, TablePagination, TableSearch, TableToolbar, usePaging } from "@/components/app/data-table"
import { CopyField } from "@/components/app/copy-button"
import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { FormField } from "@/components/app/form-field"
import { PageHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { TableSkeleton } from "@/components/app/table-skeleton"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { ApiError, errorMessage } from "@/lib/api"
import { useOrg } from "@/lib/org"
import { useApiTokens, useChangePassword, useCreateApiToken, useExportRecoveryKey, useRevokeApiToken, useRevokeSession, useSessions } from "@/lib/queries"
import type { ApiToken, Session } from "@/lib/types"

import { SettingsNav } from "./settings-nav"
import { TwoFactorCard } from "./two-factor-card"

export function SecuritySettings() {
  const { can } = useOrg()
  return (
    <div>
      <PageHeader title="Security" description="Password, two-factor authentication, sessions, API tokens for the CLI, and your backup recovery key." />
      <SettingsNav />
      <div className="space-y-6">
        <PasswordCard />
        <TwoFactorCard />
        <SessionsCard />
        <TokensCard />
        {can("owner") && <RecoveryKeyCard />}
      </div>
    </div>
  )
}

const passwordSchema = z.object({
  current_password: z.string().min(1, "Enter your current password."),
  new_password: z.string().min(10, "Use at least 10 characters.").max(256),
})
type PasswordValues = z.infer<typeof passwordSchema>

function PasswordCard() {
  const change = useChangePassword()
  const form = useForm<PasswordValues>({ resolver: zodResolver(passwordSchema), defaultValues: { current_password: "", new_password: "" } })
  return (
    <Card>
      <form
        noValidate
        onSubmit={form.handleSubmit((v) =>
          change.mutate(v, {
            onSuccess: () => {
              toast.success("Password changed", { description: "Your other sessions were signed out." })
              form.reset()
            },
            onError: (err) => {
              if (err instanceof ApiError && Object.keys(err.fields).length) {
                for (const [k, message] of Object.entries(err.fields)) form.setError(k as keyof PasswordValues, { message })
              } else toast.error(errorMessage(err))
            },
          }),
        )}
      >
        <CardHeader>
          <CardTitle>Change password</CardTitle>
          <CardDescription>Every other session is signed out when you change your password.</CardDescription>
        </CardHeader>
        <CardContent className="pt-4">
          <FieldGroup className="grid gap-4 sm:grid-cols-2">
            <FormField id="pw-current" label="Current password" error={form.formState.errors.current_password}>
              <Input id="pw-current" type="password" autoComplete="current-password" {...form.register("current_password")} />
            </FormField>
            <FormField id="pw-new" label="New password" description="At least 10 characters." error={form.formState.errors.new_password}>
              <Input id="pw-new" type="password" autoComplete="new-password" {...form.register("new_password")} />
            </FormField>
          </FieldGroup>
        </CardContent>
        <CardFooter className="mt-4 justify-end border-t pt-4">
          <Button type="submit" disabled={change.isPending}>
            {change.isPending && <Spinner />} Update password
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}

function summarizeAgent(ua: string | null): string {
  if (!ua) return "Unknown device"
  const browser = /Edg\//.test(ua)
    ? "Edge"
    : /Firefox\//.test(ua)
      ? "Firefox"
      : /Chrome\//.test(ua)
        ? "Chrome"
        : /Safari\//.test(ua)
          ? "Safari"
          : /curl/.test(ua)
            ? "curl"
            : "Browser"
  const os = /Windows/.test(ua)
    ? "Windows"
    : /Mac OS X/.test(ua)
      ? "macOS"
      : /Android/.test(ua)
        ? "Android"
        : /iPhone|iPad/.test(ua)
          ? "iOS"
          : /Linux/.test(ua)
            ? "Linux"
            : ""
  return os ? `${browser} on ${os}` : browser
}

function SessionsCard() {
  const sessions = useSessions()
  const revoke = useRevokeSession()
  const [target, setTarget] = useState<Session>()
  const [search, setSearch] = useState("")
  const rows = useMemo(() => (sessions.data ?? []).filter((s) => matches(search, summarizeAgent(s.user_agent), s.ip_address)), [sessions.data, search])
  const paging = usePaging(rows, search)
  return (
    <Card>
      <CardHeader>
        <CardTitle>Active sessions</CardTitle>
        <CardDescription>Browsers signed in to your account. Sessions expire after 14 days of inactivity.</CardDescription>
      </CardHeader>
      <CardContent>
        {sessions.error ? (
          <ErrorState error={sessions.error} retry={() => sessions.refetch()} />
        ) : sessions.isPending ? (
          <TableSkeleton rows={2} columns={4} />
        ) : (
          <div className="space-y-3">
            {(sessions.data?.length ?? 0) > 1 && (
              <TableToolbar>
                <TableSearch value={search} onChange={setSearch} placeholder="Search device or IP…" />
              </TableToolbar>
            )}
            <ul className="divide-y rounded-lg border">
              {paging.rows.map((s) => (
                <li key={s.id} className="flex flex-wrap items-center gap-3 px-3 py-2.5 text-sm">
                  <Laptop className="size-4 text-muted-foreground" />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 font-medium">
                      {summarizeAgent(s.user_agent)}
                      {s.current && <Badge variant="secondary">This device</Badge>}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      <span className="font-mono">{s.ip_address ?? "unknown IP"}</span> · last seen <RelativeTime date={s.last_seen_at} />
                    </div>
                  </div>
                  {!s.current && (
                    <Button variant="ghost" size="sm" onClick={() => setTarget(s)}>
                      Revoke
                    </Button>
                  )}
                </li>
              ))}
            </ul>
            <TablePagination {...paging.props} noun="sessions" />
          </div>
        )}
      </CardContent>
      <ConfirmDialog
        open={!!target}
        onOpenChange={(o) => !o && setTarget(undefined)}
        title="Revoke session?"
        description={<p>{summarizeAgent(target?.user_agent ?? null)} will be signed out immediately.</p>}
        confirmLabel="Revoke session"
        pending={revoke.isPending}
        onConfirm={() =>
          target &&
          revoke.mutate(target.id, {
            onSuccess: () => {
              toast.success("Session revoked")
              setTarget(undefined)
            },
            onError: (e) => toast.error(errorMessage(e)),
          })
        }
      />
    </Card>
  )
}

function TokensCard() {
  const tokens = useApiTokens()
  const [search, setSearch] = useState("")
  const rows = useMemo(() => (tokens.data ?? []).filter((t) => matches(search, t.name, t.prefix)), [tokens.data, search])
  const paging = usePaging(rows, search)
  const revoke = useRevokeApiToken()
  const [creating, setCreating] = useState(false)
  const [target, setTarget] = useState<ApiToken>()
  return (
    <Card>
      <CardHeader>
        <CardTitle>API tokens</CardTitle>
        <CardDescription>For the dbvault CLI and automation. Tokens act as you, in every organization you belong to.</CardDescription>
        <CardAction>
          <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
            <Plus /> Create token
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {tokens.error ? (
          <ErrorState error={tokens.error} retry={() => tokens.refetch()} />
        ) : tokens.isPending ? (
          <TableSkeleton rows={2} columns={4} />
        ) : tokens.data?.length === 0 ? (
          <EmptyState
            icon={KeyRound}
            title="No API tokens"
            description="Create a token to use the dbvault CLI, or run `dbvault init` to sign in."
            className="py-10"
          />
        ) : (
          <div className="space-y-3">
            {(tokens.data?.length ?? 0) > 1 && (
              <TableToolbar>
                <TableSearch value={search} onChange={setSearch} placeholder="Search tokens…" />
              </TableToolbar>
            )}
            <div className="overflow-hidden rounded-lg border">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="pl-3">Name</TableHead>
                    <TableHead>Token</TableHead>
                    <TableHead className="hidden sm:table-cell">Last used</TableHead>
                    <TableHead className="hidden sm:table-cell">Expires</TableHead>
                    <TableHead className="pr-3" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {paging.rows.map((t) => (
                    <TableRow key={t.id}>
                      <TableCell className="pl-3 font-medium">{t.name}</TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">{t.prefix}…</TableCell>
                      <TableCell className="hidden text-muted-foreground sm:table-cell">
                        {t.last_used_at ? <RelativeTime date={t.last_used_at} /> : "Never"}
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground sm:table-cell">
                        {t.expires_at ? <RelativeTime date={t.expires_at} /> : "Never"}
                      </TableCell>
                      <TableCell className="pr-3 text-right">
                        <Button variant="ghost" size="sm" onClick={() => setTarget(t)}>
                          Revoke
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            <TablePagination {...paging.props} noun="tokens" />
          </div>
        )}
      </CardContent>
      <CreateTokenDialog open={creating} onOpenChange={setCreating} />
      <ConfirmDialog
        open={!!target}
        onOpenChange={(o) => !o && setTarget(undefined)}
        title={`Revoke ${target?.name}?`}
        description={<p>Anything using this token (CLI, CI jobs) stops working immediately.</p>}
        confirmLabel="Revoke token"
        pending={revoke.isPending}
        onConfirm={() =>
          target &&
          revoke.mutate(target.id, {
            onSuccess: () => {
              toast.success("Token revoked")
              setTarget(undefined)
            },
            onError: (e) => toast.error(errorMessage(e)),
          })
        }
      />
    </Card>
  )
}

const CLI_INSTALL =
  "git clone --depth 1 https://github.com/Database-Vault-Tech/dbvault.git && cd dbvault/cli && go build -o dbvault . && sudo mv dbvault /usr/local/bin/"

function CliStep({ n, title, children }: { n: number; title: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 gap-3">
      <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-medium tabular-nums">{n}</span>
      <div className="min-w-0 flex-1 space-y-1.5">
        <div className="text-xs text-muted-foreground">{title}</div>
        {children}
      </div>
    </div>
  )
}

function CreateTokenDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const create = useCreateApiToken()
  const [name, setName] = useState("")
  const [expiry, setExpiry] = useState("90")
  const [token, setToken] = useState<string | null>(null)
  const server = typeof window !== "undefined" ? window.location.origin : "http://localhost:3000"
  const close = (o: boolean) => {
    onOpenChange(o)
    if (!o) {
      setToken(null)
      setName("")
    }
  }
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="grid-cols-1 sm:max-w-xl">
        {token ? (
          <>
            <DialogHeader>
              <DialogTitle>Your new API token</DialogTitle>
              <DialogDescription>Copy it now — for your security it won&apos;t be shown again.</DialogDescription>
            </DialogHeader>
            <Alert>
              <AlertTriangle />
              <AlertDescription>Treat this token like a password. It can act as you in every organization you belong to.</AlertDescription>
            </Alert>
            <div className="min-w-0 space-y-1.5">
              <div className="text-xs font-medium text-muted-foreground">Token</div>
              <CopyField value={token} wrap />
            </div>
            <div className="min-w-0 space-y-4 border-t pt-4">
              <div className="text-sm font-medium">Set up the CLI</div>
              <CliStep n={1} title="Install the dbvault binary (needs Go)">
                <CopyField value={CLI_INSTALL} wrap />
              </CliStep>
              <CliStep n={2} title="Connect it to this server">
                <CopyField value={`dbvault init --server ${server} --token ${token}`} wrap />
              </CliStep>
              <p className="text-xs text-muted-foreground">
                In CI, skip <code className="font-mono">init</code> and set <code className="font-mono">DBVAULT_SERVER</code> and{" "}
                <code className="font-mono">DBVAULT_TOKEN</code> instead.
              </p>
            </div>
            <DialogFooter>
              <Button onClick={() => close(false)}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <form
            className="space-y-5"
            onSubmit={(e) => {
              e.preventDefault()
              create.mutate(
                { name: name.trim(), expires_in_days: Number(expiry) },
                { onSuccess: (r) => setToken(r.token), onError: (err) => toast.error(errorMessage(err)) },
              )
            }}
          >
            <DialogHeader>
              <DialogTitle>Create API token</DialogTitle>
              <DialogDescription>Name it after where it will be used, e.g. &quot;laptop&quot; or &quot;GitHub Actions&quot;.</DialogDescription>
            </DialogHeader>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="tok-name">Name</FieldLabel>
                <Input id="tok-name" value={name} maxLength={100} autoFocus onChange={(e) => setName(e.target.value)} />
              </Field>
              <Field>
                <FieldLabel htmlFor="tok-exp">Expires</FieldLabel>
                <Select value={expiry} onValueChange={setExpiry}>
                  <SelectTrigger id="tok-exp" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="30">In 30 days</SelectItem>
                    <SelectItem value="90">In 90 days</SelectItem>
                    <SelectItem value="365">In 1 year</SelectItem>
                    <SelectItem value="0">Never</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            </FieldGroup>
            <DialogFooter>
              <Button type="submit" disabled={!name.trim() || create.isPending}>
                {create.isPending && <Spinner />} Create token
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

function RecoveryKeyCard() {
  const exportKey = useExportRecoveryKey()
  const [open, setOpen] = useState(false)
  const [password, setPassword] = useState("")
  const key = exportKey.data
  const close = (o: boolean) => {
    setOpen(o)
    if (!o) {
      setPassword("")
      exportKey.reset()
    }
  }
  const download = () => {
    if (!key) return
    const body = `# DBVault recovery key (age X25519 identity)\n# Key ID: ${key.key_id}\n# Public key: ${key.public_key}\n# Created: ${new Date().toISOString()}\n# Decrypt: age -d -i recovery.key backup.dump.zst.age | zstd -d > backup.dump\n${key.identity}\n`
    const url = URL.createObjectURL(new Blob([body], { type: "text/plain" }))
    const a = document.createElement("a")
    a.href = url
    a.download = "recovery.key"
    a.click()
    URL.revokeObjectURL(url)
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>Backup recovery key</CardTitle>
        <CardDescription>
          Backups are encrypted with this organization&apos;s age key, which the server seals with its ENCRYPTION_KEY. Export it and store it offline (a
          password manager or safe) so backups can be decrypted with the standard <code className="font-mono">age</code> CLI even if DBVault itself is lost.
        </CardDescription>
        <CardAction>
          <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
            <KeyRound /> Export recovery key
          </Button>
        </CardAction>
      </CardHeader>
      <Dialog open={open} onOpenChange={close}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>Export recovery key</DialogTitle>
            <DialogDescription>This export is recorded in the audit log.</DialogDescription>
          </DialogHeader>
          {key ? (
            <div className="space-y-4">
              <Alert variant="destructive">
                <AlertTriangle />
                <AlertTitle>Anyone with this key can decrypt every backup</AlertTitle>
                <AlertDescription>Never commit it, paste it in chat, or store it next to the backups.</AlertDescription>
              </Alert>
              <CopyField value={key.identity} />
              <Button variant="outline" onClick={download}>
                <Download /> Download recovery.key
              </Button>
              <pre className="overflow-x-auto rounded-lg border bg-zinc-950 p-3 font-mono text-xs text-zinc-100">
                {`age -d -i recovery.key backup.dump.zst.age | zstd -d > backup.dump \\
  && pg_restore -d mydb backup.dump`}
              </pre>
            </div>
          ) : (
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault()
                exportKey.mutate(password, { onError: (err) => toast.error(errorMessage(err)) })
              }}
            >
              <Field>
                <FieldLabel htmlFor="rk-pw">Confirm your password</FieldLabel>
                <Input id="rk-pw" type="password" autoComplete="current-password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />
              </Field>
              <DialogFooter>
                <Button type="submit" disabled={!password || exportKey.isPending}>
                  {exportKey.isPending && <Spinner />} Reveal key
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </Card>
  )
}
