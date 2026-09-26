"use client"

import { zodResolver } from "@hookform/resolvers/zod"
import { CheckCircle2, ClipboardPaste, Database, FileText, FolderOpen, Lock, PlugZap, XCircle } from "lucide-react"
import { useEffect, useState } from "react"
import { Controller, useForm, useWatch } from "react-hook-form"

import { FormField } from "@/components/app/form-field"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { FieldGroup } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Textarea } from "@/components/ui/textarea"
import { ApiError, errorMessage } from "@/lib/api"
import { ENGINE_LIST, ENGINES, engineMeta, type SupportedEngine } from "@/lib/engines"
import { formatBytes, formatNumber } from "@/lib/format"
import { cn } from "@/lib/utils"
import { useDatabaseEngines, useTestConnection } from "@/lib/queries"
import { databaseSchema, parseConnectionString, type DatabaseValues } from "@/lib/schemas"
import type { ConnectionTest, DatabaseInput, SSLMode } from "@/lib/types"

const SSL_HELP: Record<SSLMode, string> = {
  disable: "No TLS. Only for trusted private networks.",
  allow: "Try without TLS first, fall back to TLS.",
  prefer: "Use TLS when the server supports it (default).",
  require: "Always use TLS; don't verify the certificate.",
  "verify-ca": "TLS and verify the certificate against a CA.",
  "verify-full": "TLS, verify the CA and the hostname. Most secure.",
}

function sslHelp(engine: SupportedEngine, mode: SSLMode): string {
  if (engine !== "postgres" && mode === "verify-full") return "TLS, verify the certificate and the hostname. Most secure."
  return SSL_HELP[mode]
}

export function toInput(v: DatabaseValues): DatabaseInput {
  if (ENGINES[v.engine].fileBased) {
    return { engine: v.engine, name: v.name, host: "", port: 0, database: v.database, username: "", ssl_mode: "disable" }
  }
  return {
    engine: v.engine,
    name: v.name,
    host: v.host,
    port: Number(v.port),
    database: v.database,
    username: v.username,
    password: v.password || undefined,
    ssl_mode: v.ssl_mode,
    ssl_root_cert: v.ssl_mode === "verify-ca" || v.ssl_mode === "verify-full" ? v.ssl_root_cert || undefined : undefined,
  }
}

export function ConnectionResult({ result, engine = "postgres" }: { result: ConnectionTest; engine?: string }) {
  const meta = engineMeta(engine)
  if (!result.ok) {
    return (
      <Alert variant="destructive">
        <XCircle />
        <AlertTitle>Connection failed</AlertTitle>
        <AlertDescription>{result.message}</AlertDescription>
      </Alert>
    )
  }
  const s = result.server
  return (
    <Alert className="border-success/30 bg-success/5">
      <CheckCircle2 className="text-success" />
      <AlertTitle className="text-success">Connection successful</AlertTitle>
      {s && (
        <AlertDescription>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm tabular">
            <span className="font-medium text-foreground">
              {meta.label} {s.version}
            </span>
            <span>{formatBytes(s.size_bytes)}</span>
            <span>{formatNumber(s.table_count)} tables</span>
            <span>{s.latency_ms} ms</span>
            {s.current_user && <span>as {s.current_user}</span>}
          </div>
          {s.is_superuser && meta.id === "postgres" && (
            <p className="mt-1 text-xs">This user is a superuser. A read-only role with pg_read_all_data is enough for backups.</p>
          )}
          {s.in_recovery && (
            <p className="mt-1 text-xs">
              {meta.id === "postgres"
                ? "This server is a replica — great for taking backups without loading the primary."
                : "This server is read-only (usually a replica) — great for taking backups without loading the primary."}
            </p>
          )}
        </AlertDescription>
      )}
    </Alert>
  )
}

export function DatabaseForm({
  mode,
  databaseId,
  defaultValues,
  submitLabel,
  onSubmit,
  onCancel,
  onEngineChange,
}: {
  mode: "create" | "edit"
  databaseId?: string
  defaultValues?: Partial<DatabaseValues>
  submitLabel: string
  onSubmit: (input: DatabaseInput) => Promise<unknown>
  onCancel?: () => void
  onEngineChange?: (engine: SupportedEngine) => void
}) {
  const form = useForm<DatabaseValues>({
    resolver: zodResolver(databaseSchema(mode === "create")),
    defaultValues: {
      engine: "postgres",
      name: "",
      host: "",
      port: 5432,
      database: "postgres",
      username: "",
      password: "",
      ssl_mode: "prefer",
      ssl_root_cert: "",
      ...defaultValues,
    },
  })
  const test = useTestConnection()
  const [result, setResult] = useState<ConnectionTest | null>(null)
  const [connString, setConnString] = useState("")
  const [submitError, setSubmitError] = useState<string | null>(null)
  const errors = form.formState.errors
  const sslMode = useWatch({ control: form.control, name: "ssl_mode" })
  const host = useWatch({ control: form.control, name: "host" })
  const engine = useWatch({ control: form.control, name: "engine" })
  const meta = ENGINES[engine] ?? ENGINES.postgres
  const engines = useDatabaseEngines()
  const unavailable = engines.data?.find((e) => e.name === engine && !e.available)?.unavailable_reason

  useEffect(() => {
    onEngineChange?.(engine)
  }, [engine, onEngineChange])

  // Switching engines swaps defaults the user hasn't changed.
  const selectEngine = (next: SupportedEngine) => {
    const prev = ENGINES[form.getValues("engine")]
    const to = ENGINES[next]
    if (prev.id === to.id) return
    form.setValue("engine", next, { shouldDirty: true })
    if (Number(form.getValues("port")) === prev.defaultPort || !form.getValues("port")) form.setValue("port", to.defaultPort)
    // A file path and a database name are different things: don't carry one over.
    if (form.getValues("database") === prev.defaultDatabase || prev.fileBased !== to.fileBased) form.setValue("database", to.defaultDatabase)
    if (to.fileBased) form.setValue("ssl_mode", "disable")
    else if (prev.fileBased || !(to.sslModes as readonly string[]).includes(form.getValues("ssl_mode"))) form.setValue("ssl_mode", "prefer")
    form.clearErrors()
    setResult(null)
  }

  const applyFieldErrors = (err: unknown) => {
    if (err instanceof ApiError && Object.keys(err.fields).length) {
      for (const [f, message] of Object.entries(err.fields)) form.setError(f as keyof DatabaseValues, { message })
      return true
    }
    return false
  }

  const runTest = form.handleSubmit(async (values) => {
    setResult(null)
    try {
      setResult(await test.mutateAsync({ ...toInput(values), database_id: databaseId }))
    } catch (err) {
      if (!applyFieldErrors(err)) setResult({ ok: false, message: errorMessage(err), tested_at: new Date().toISOString() })
    }
  })

  const submit = form.handleSubmit(async (values) => {
    setSubmitError(null)
    try {
      await onSubmit(toInput(values))
    } catch (err) {
      if (!applyFieldErrors(err)) setSubmitError(errorMessage(err))
    }
  })

  const paste = (raw: string) => {
    setConnString(raw)
    const parsed = parseConnectionString(raw)
    if (!parsed) return
    for (const [k, v] of Object.entries(parsed)) form.setValue(k as keyof DatabaseValues, v as never, { shouldValidate: true })
    if (!form.getValues("name") && parsed.database) form.setValue("name", parsed.database)
  }

  return (
    <form onSubmit={submit} noValidate className="space-y-6">
      {mode === "create" && !meta.fileBased && (
        <FormField id="conn-string" label="Paste a connection string (optional)" description="Fills the fields below. It's parsed in your browser and never sent as-is.">
          <div className="relative">
            <ClipboardPaste className="pointer-events-none absolute top-2 left-2.5 size-4 text-muted-foreground" />
            <Input
              id="conn-string"
              className="pl-8 font-mono text-xs"
              placeholder={meta.urlExample}
              value={connString}
              onChange={(e) => paste(e.target.value)}
              autoComplete="off"
            />
          </div>
        </FormField>
      )}
      <FieldGroup>
        {mode === "create" ? (
          <FormField id="engine" label="Database type" error={errors.engine}>
            <div id="engine" role="radiogroup" aria-label="Database type" className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
              {ENGINE_LIST.map((e) => {
                const selected = e.id === engine
                return (
                  <button
                    key={e.id}
                    type="button"
                    role="radio"
                    aria-checked={selected}
                    onClick={() => selectEngine(e.id)}
                    className={cn(
                      "flex items-center justify-between gap-2 rounded-lg border px-3 py-2.5 text-left text-sm transition-colors outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50",
                      selected ? "border-brand bg-brand/5 ring-1 ring-brand" : "hover:bg-muted/50",
                    )}
                  >
                    <span className="flex items-center gap-2">
                      <Database className={cn("size-4", selected ? "text-brand" : "text-muted-foreground")} aria-hidden />
                      <span className="font-medium">{e.label}</span>
                    </span>
                    <span className="font-mono text-[11px] text-muted-foreground">{e.fileBased ? "file" : e.defaultPort}</span>
                  </button>
                )
              })}
            </div>
          </FormField>
        ) : (
          <FormField id="engine" label="Database type" description="The database type can't be changed after it's added.">
            <Input id="engine" value={meta.label} disabled readOnly />
          </FormField>
        )}
        <FormField id="name" label="Name" description="How this database appears in DBVault, e.g. production." error={errors.name}>
          <Input id="name" placeholder="production" {...form.register("name")} />
        </FormField>
        {unavailable && (
          <Alert className="border-warning/40 bg-warning/5">
            <FolderOpen className="text-warning" />
            <AlertTitle>{meta.label} isn&apos;t set up on this server yet</AlertTitle>
            <AlertDescription>{unavailable}</AlertDescription>
          </Alert>
        )}
        {meta.fileBased ? (
          <FormField
            id="database"
            label="Database file"
            description="Path inside the SQLite folder mounted into DBVault (SQLITE_ROOT), e.g. myapp/app.db."
            error={errors.database}
          >
            <div className="relative">
              <FileText className="pointer-events-none absolute top-2 left-2.5 size-4 text-muted-foreground" />
              <Input id="database" className="pl-8 font-mono" placeholder="myapp/app.db" autoComplete="off" {...form.register("database")} />
            </div>
          </FormField>
        ) : (
          <>
            <div className="grid gap-4 sm:grid-cols-[1fr_120px]">
              <FormField
                id="host"
                label="Host"
                error={errors.host}
                description={
                  /^(localhost|127\.0\.0\.1|::1)$/i.test(host?.trim() ?? "")
                    ? "DBVault runs in Docker, where localhost is the DBVault container. For a database on this machine use host.docker.internal."
                    : undefined
                }
              >
                <Input id="host" className="font-mono" placeholder="db.example.com" autoComplete="off" {...form.register("host")} />
              </FormField>
              <FormField id="port" label="Port" error={errors.port}>
                <Input id="port" type="number" inputMode="numeric" className="font-mono" {...form.register("port")} />
              </FormField>
            </div>
            <FormField id="database" label="Database" error={errors.database}>
              <Input id="database" className="font-mono" autoComplete="off" {...form.register("database")} />
            </FormField>
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField id="username" label="Username" error={errors.username}>
                <Input id="username" className="font-mono" autoComplete="off" {...form.register("username")} />
              </FormField>
              <FormField id="password" label="Password" error={errors.password}>
                <Input
                  id="password"
                  type="password"
                  autoComplete="new-password"
                  placeholder={mode === "edit" ? "Leave blank to keep the current password" : ""}
                  {...form.register("password")}
                />
              </FormField>
            </div>
            <FormField id="ssl_mode" label="SSL mode" description={sslHelp(engine, sslMode)} error={errors.ssl_mode}>
              <Controller
                control={form.control}
                name="ssl_mode"
                render={({ field }) => (
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="ssl_mode" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {meta.sslModes.map((m) => (
                        <SelectItem key={m} value={m}>
                          <span className="font-mono">{m}</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              />
            </FormField>
            {(sslMode === "verify-ca" || sslMode === "verify-full") && (
              <FormField
                id="ssl_root_cert"
                label="CA certificate (PEM)"
                description={mode === "edit" ? "Leave blank to keep the stored certificate." : "The root certificate used to verify the server."}
                error={errors.ssl_root_cert}
              >
                <Textarea id="ssl_root_cert" rows={5} className="font-mono text-xs" placeholder="-----BEGIN CERTIFICATE-----" {...form.register("ssl_root_cert")} />
              </FormField>
            )}
          </>
        )}
      </FieldGroup>

      {result && <ConnectionResult result={result} engine={engine} />}
      {submitError && (
        <Alert variant="destructive">
          <AlertDescription>{submitError}</AlertDescription>
        </Alert>
      )}

      <p className="flex items-start gap-2 text-xs text-muted-foreground">
        <Lock className="mt-0.5 size-3.5 shrink-0" />
        {meta.fileBased
          ? "DBVault only reads and writes files inside the SQLite folder. Backups are consistent snapshots taken while your app keeps running."
          : "Credentials are encrypted with AES-256-GCM before they're stored and are never shown again."}
      </p>

      <div className="flex flex-wrap items-center justify-end gap-2 border-t pt-4">
        {onCancel && (
          <Button type="button" variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        )}
        <Button type="button" variant="outline" onClick={runTest} disabled={test.isPending}>
          {test.isPending ? <Spinner /> : <PlugZap />} Test Connection
        </Button>
        <Button type="submit" disabled={form.formState.isSubmitting}>
          {form.formState.isSubmitting && <Spinner />} {submitLabel}
        </Button>
      </div>
    </form>
  )
}
