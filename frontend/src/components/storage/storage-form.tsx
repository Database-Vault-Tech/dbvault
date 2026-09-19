"use client"

import { zodResolver } from "@hookform/resolvers/zod"
import { Cloud, HardDrive, Lock, Server, Sparkles } from "lucide-react"
import { useState } from "react"
import { useForm, useWatch } from "react-hook-form"
import { toast } from "sonner"
import { z } from "zod"

import { FormField } from "@/components/app/form-field"
import { StatusBadge } from "@/components/app/status"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { ApiError, errorMessage } from "@/lib/api"
import { useBuiltinStorage, useCreateStorage, useTestStorageInput, useUpdateStorage } from "@/lib/queries"
import type { StorageDestination, StorageInput, StorageTest, StorageType } from "@/lib/types"
import { cn } from "@/lib/utils"

const TYPES: { type: StorageType; label: string; description: string; icon: typeof Cloud }[] = [
  { type: "s3", label: "Amazon S3", description: "AWS S3 or any S3-compatible API", icon: Cloud },
  { type: "r2", label: "Cloudflare R2", description: "Zero egress fees", icon: Cloud },
  { type: "minio", label: "MinIO", description: "Self-hosted object storage", icon: Server },
  { type: "local", label: "Local filesystem", description: "A directory on the worker volume", icon: HardDrive },
]

const schema = z
  .object({
    name: z
      .string()
      .trim()
      .min(1, "Give this destination a name.")
      .max(63)
      .regex(/^[a-zA-Z0-9][a-zA-Z0-9 _.-]*$/, "Use letters, numbers, spaces, dots, dashes or underscores."),
    type: z.enum(["s3", "r2", "minio", "local"]),
    bucket: z.string().trim(),
    region: z.string().trim(),
    endpoint: z.string().trim(),
    account_id: z.string().trim(),
    prefix: z.string().trim(),
    path: z.string().trim(),
    access_key_id: z.string().trim(),
    secret_access_key: z.string().trim(),
    is_default: z.boolean(),
  })
  .superRefine((v, ctx) => {
    const req = (field: keyof typeof v, message: string) => {
      if (!String(v[field] ?? "")) ctx.addIssue({ code: "custom", path: [field], message })
    }
    if (v.type !== "local") {
      req("bucket", "Enter the bucket name.")
      if (v.bucket && !/^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$/.test(v.bucket))
        ctx.addIssue({ code: "custom", path: ["bucket"], message: "3-63 lowercase letters, numbers, dots and dashes." })
    }
    if (v.type === "s3") req("region", "Enter the region, e.g. eu-west-1.")
    if (v.type === "minio") req("endpoint", "Enter the MinIO endpoint, e.g. http://minio:9000.")
    if (v.type === "r2" && !v.endpoint && !/^[a-f0-9]{32}$/.test(v.account_id))
      ctx.addIssue({ code: "custom", path: ["account_id"], message: "Enter your 32-character Cloudflare account ID." })
    if (v.endpoint && !/^https?:\/\/[^\s/]+/.test(v.endpoint))
      ctx.addIssue({ code: "custom", path: ["endpoint"], message: "Must be an http(s) URL." })
    if (v.path.includes("..")) ctx.addIssue({ code: "custom", path: ["path"], message: "Must stay inside the storage root." })
  })

type Values = z.infer<typeof schema>

function toInput(v: Values): StorageInput {
  const input: StorageInput = {
    name: v.name,
    type: v.type,
    is_default: v.is_default,
    config:
      v.type === "local"
        ? { path: v.path, prefix: v.prefix }
        : { bucket: v.bucket, region: v.region || undefined, endpoint: v.endpoint || undefined, account_id: v.account_id || undefined, prefix: v.prefix },
  }
  if (v.type !== "local" && v.access_key_id && v.secret_access_key) {
    input.access_key_id = v.access_key_id
    input.secret_access_key = v.secret_access_key
  }
  return input
}

function defaults(d?: StorageDestination): Values {
  return {
    name: d?.name ?? "",
    type: d?.type ?? "s3",
    bucket: d?.config.bucket ?? "",
    region: d?.config.region ?? "",
    endpoint: d?.config.endpoint ?? "",
    account_id: d?.config.account_id ?? "",
    prefix: d?.config.prefix ?? "",
    path: d?.config.path ?? "",
    access_key_id: "",
    secret_access_key: "",
    is_default: d?.is_default ?? false,
  }
}

export function StorageDialog({ open, onOpenChange, destination }: { open: boolean; onOpenChange: (o: boolean) => void; destination?: StorageDestination }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
        {open && <StorageForm destination={destination} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  )
}

function StorageForm({ destination, onDone }: { destination?: StorageDestination; onDone: () => void }) {
  const editing = !!destination
  const builtin = useBuiltinStorage()
  const create = useCreateStorage()
  const update = useUpdateStorage(destination?.id ?? "")
  const test = useTestStorageInput()
  const [testResult, setTestResult] = useState<StorageTest | null>(null)
  const [builtinPrefix, setBuiltinPrefix] = useState("")
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: defaults(destination) })
  const [type, isDefault] = useWatch({ control: form.control, name: ["type", "is_default"] })
  const errors = form.formState.errors

  const applyApiErrors = (err: unknown) => {
    if (err instanceof ApiError && Object.keys(err.fields).length) {
      for (const [k, message] of Object.entries(err.fields)) {
        const field = k.replace(/^config\./, "") as keyof Values
        form.setError(field in form.getValues() ? field : "name", { message })
      }
    } else toast.error("Couldn't save storage", { description: errorMessage(err) })
  }

  const onSubmit = form.handleSubmit((values) => {
    const input = toInput(values)
    if (!editing && values.type !== "local" && !input.access_key_id) {
      form.setError("secret_access_key", { message: "Enter both the access key and the secret key." })
      return
    }
    if (editing) {
      update.mutate(input, {
        onSuccess: () => {
          toast.success(`Saved ${values.name}`)
          onDone()
        },
        onError: applyApiErrors,
      })
    } else {
      create.mutate(input, {
        onSuccess: ({ storage, test }) => {
          if (test.ok) toast.success(`Added ${storage.name}`, { description: "Connection test passed: write, read and delete succeeded." })
          else toast.warning(`Added ${storage.name}, but the connection test failed`, { description: test.message })
          onDone()
        },
        onError: applyApiErrors,
      })
    }
  })

  const runTest = form.handleSubmit((values) => {
    setTestResult(null)
    test.mutate({ ...toInput(values), storage_id: destination?.id }, { onSuccess: setTestResult, onError: (e) => toast.error(errorMessage(e)) })
  })

  const addBuiltin = () =>
    create.mutate(
      { name: "Local MinIO", type: "minio", config: { prefix: builtinPrefix.trim() }, use_builtin: true },
      {
        onSuccess: ({ storage, test }) => {
          if (test.ok) toast.success(`Added ${storage.name}`, { description: "Connection test passed." })
          else toast.warning("Added built-in storage, but the connection test failed", { description: test.message })
          onDone()
        },
        onError: (e) => toast.error("Couldn't add built-in storage", { description: errorMessage(e) }),
      },
    )

  const pending = create.isPending || update.isPending

  return (
    <form onSubmit={onSubmit} noValidate className="space-y-5">
      <DialogHeader>
        <DialogTitle>{editing ? `Edit ${destination.name}` : "Add storage destination"}</DialogTitle>
        <DialogDescription>Where DBVault uploads encrypted backup files. Credentials are encrypted at rest with AES-256-GCM and never shown again.</DialogDescription>
      </DialogHeader>

      {!editing && builtin.data?.available && (
        <div className="space-y-3 rounded-xl border border-brand/40 bg-brand/5 p-4">
          <div className="flex items-start gap-3">
            <Sparkles className="mt-0.5 size-4 text-brand" />
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium">Built-in MinIO</div>
              <p className="text-sm text-muted-foreground">
                One click: use the MinIO bundled with docker compose (bucket <span className="font-mono">{builtin.data.bucket}</span>).
              </p>
            </div>
          </div>
          <div className="flex flex-col gap-2 sm:flex-row">
            <Input placeholder="Optional prefix, e.g. production" value={builtinPrefix} onChange={(e) => setBuiltinPrefix(e.target.value)} />
            <Button type="button" onClick={addBuiltin} disabled={create.isPending}>
              {create.isPending && <Spinner />} Use built-in storage
            </Button>
          </div>
        </div>
      )}

      <FieldGroup>
        {!editing && (
          <Field>
            <FieldLabel>Provider</FieldLabel>
            <div className="grid grid-cols-2 gap-2">
              {TYPES.map((t) => (
                <button
                  type="button"
                  key={t.type}
                  onClick={() => {
                    form.setValue("type", t.type)
                    setTestResult(null)
                  }}
                  className={cn(
                    "flex items-start gap-2.5 rounded-lg border p-3 text-left transition-colors hover:bg-muted/60",
                    type === t.type && "border-foreground/40 bg-muted/60 ring-1 ring-foreground/20",
                  )}
                >
                  <t.icon className="mt-0.5 size-4 text-muted-foreground" />
                  <span>
                    <span className="block text-sm font-medium">{t.label}</span>
                    <span className="block text-xs text-muted-foreground">{t.description}</span>
                  </span>
                </button>
              ))}
            </div>
          </Field>
        )}
        <FormField id="st-name" label="Name" error={errors.name}>
          <Input id="st-name" placeholder={type === "local" ? "Server disk" : "Production backups"} {...form.register("name")} />
        </FormField>

        {type === "local" ? (
          <FormField
            id="st-path"
            label="Directory"
            description="Relative to LOCAL_STORAGE_ROOT on the API/worker volume (e.g. /var/lib/dbvault/backups). Use off-site storage for real protection."
            error={errors.path}
          >
            <Input id="st-path" placeholder="production" className="font-mono" {...form.register("path")} />
          </FormField>
        ) : (
          <>
            {type === "r2" && (
              <FormField id="st-account" label="Cloudflare account ID" description="Found in the R2 dashboard sidebar." error={errors.account_id}>
                <Input id="st-account" className="font-mono" placeholder="32 hex characters" {...form.register("account_id")} />
              </FormField>
            )}
            {type === "minio" && (
              <FormField id="st-endpoint" label="Endpoint" error={errors.endpoint}>
                <Input id="st-endpoint" className="font-mono" placeholder="http://minio:9000" {...form.register("endpoint")} />
              </FormField>
            )}
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField id="st-bucket" label="Bucket" error={errors.bucket}>
                <Input id="st-bucket" className="font-mono" placeholder="dbvault-production" {...form.register("bucket")} />
              </FormField>
              {type === "s3" && (
                <FormField id="st-region" label="Region" error={errors.region}>
                  <Input id="st-region" className="font-mono" placeholder="eu-west-1" {...form.register("region")} />
                </FormField>
              )}
            </div>
            {type === "s3" && (
              <FormField id="st-endpoint" label="Custom endpoint (optional)" description="Only for S3-compatible providers other than AWS." error={errors.endpoint}>
                <Input id="st-endpoint" className="font-mono" placeholder="https://s3.example.com" {...form.register("endpoint")} />
              </FormField>
            )}
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField id="st-ak" label="Access key ID" error={errors.access_key_id}>
                <Input
                  id="st-ak"
                  className="font-mono"
                  autoComplete="off"
                  placeholder={editing ? destination.access_key_hint || "Keep current" : ""}
                  {...form.register("access_key_id")}
                />
              </FormField>
              <FormField id="st-sk" label="Secret access key" error={errors.secret_access_key}>
                <Input
                  id="st-sk"
                  type="password"
                  autoComplete="new-password"
                  placeholder={editing ? "Leave blank to keep current credentials" : ""}
                  {...form.register("secret_access_key")}
                />
              </FormField>
            </div>
          </>
        )}
        <FormField id="st-prefix" label="Key prefix (optional)" description="Backups are stored under <prefix>/<database>/YYYY/MM/DD/." error={errors.prefix}>
          <Input id="st-prefix" className="font-mono" placeholder="dbvault" {...form.register("prefix")} />
        </FormField>
        <Field orientation="horizontal">
          <Switch id="st-default" checked={isDefault} onCheckedChange={(v) => form.setValue("is_default", v)} />
          <FieldLabel htmlFor="st-default" className="font-normal">
            Use as the default destination for manual backups
          </FieldLabel>
        </Field>
      </FieldGroup>

      {testResult && (
        <Alert variant={testResult.ok ? "default" : "destructive"}>
          <AlertDescription className="flex flex-wrap items-center gap-2">
            <StatusBadge status={testResult.ok ? "pass" : "fail"} label={testResult.ok ? "Connected" : "Failed"} />
            <span>{testResult.message}</span>
          </AlertDescription>
        </Alert>
      )}

      <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Lock className="size-3" /> Keys are sealed with the server&apos;s ENCRYPTION_KEY before they are stored.
      </p>

      <DialogFooter className="gap-2">
        <Button type="button" variant="outline" onClick={runTest} disabled={test.isPending}>
          {test.isPending && <Spinner />} Test connection
        </Button>
        <Button type="submit" disabled={pending}>
          {pending && <Spinner />} {editing ? "Save changes" : "Add storage"}
        </Button>
      </DialogFooter>
    </form>
  )
}
