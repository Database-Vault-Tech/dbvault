"use client"

import { zodResolver } from "@hookform/resolvers/zod"
import { AlertTriangle, CalendarClock } from "lucide-react"
import Link from "next/link"
import { useEffect, useMemo, useState } from "react"
import { Controller, useForm, useWatch } from "react-hook-form"
import { toast } from "sonner"
import { z } from "zod"

import { FormField } from "@/components/app/form-field"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel, FieldLegend, FieldSet } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { ApiError, errorMessage } from "@/lib/api"
import { useCreateSchedule, useDatabases, useSchedulePreview, useStorage, useUpdateSchedule } from "@/lib/queries"
import type { Schedule, SchedulePreset } from "@/lib/types"
import { cn } from "@/lib/utils"

import { allTimeZones, browserTimeZone, formatInZone } from "./utils"

const PRESETS: { value: SchedulePreset; label: string; hint: string }[] = [
  { value: "hourly", label: "Every hour", hint: "At minute 0" },
  { value: "every_6_hours", label: "Every 6 hours", hint: "00, 06, 12, 18" },
  { value: "daily", label: "Daily", hint: "At 02:00" },
  { value: "weekly", label: "Weekly", hint: "Sunday 03:00" },
  { value: "custom", label: "Custom", hint: "Cron expression" },
]

const count = (max: number) => z.coerce.number<number>().int("Whole numbers only.").min(0, "Can't be negative.").max(max, `At most ${max}.`)

const schema = z
  .object({
    database_id: z.string().min(1, "Select a database."),
    storage_destination_id: z.string().min(1, "Select a storage destination."),
    name: z.string().trim().max(80),
    preset: z.enum(["hourly", "every_6_hours", "daily", "weekly", "custom"]),
    cron_expression: z.string().trim(),
    timezone: z.string().min(1),
    compression: z.enum(["zstd", "gzip", "none"]),
    encryption: z.boolean(),
    verify_after_backup: z.boolean(),
    enabled: z.boolean(),
    retention: z.object({ daily: count(3650), weekly: count(520), monthly: count(240) }),
  })
  .refine((v) => v.preset !== "custom" || v.cron_expression.length > 0, { path: ["cron_expression"], message: "Enter a cron expression." })

type Values = z.infer<typeof schema>

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms)
    return () => clearTimeout(t)
  }, [value, ms])
  return v
}

export function ScheduleDialog({
  open,
  onOpenChange,
  schedule,
  initialDatabaseId,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  schedule?: Schedule
  initialDatabaseId?: string
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        {open && <ScheduleForm schedule={schedule} initialDatabaseId={initialDatabaseId} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  )
}

function ScheduleForm({ schedule, initialDatabaseId, onDone }: { schedule?: Schedule; initialDatabaseId?: string; onDone: () => void }) {
  const databases = useDatabases()
  const storage = useStorage()
  const create = useCreateSchedule()
  const update = useUpdateSchedule(schedule?.id ?? "")
  const zones = useMemo(() => allTimeZones(), [])

  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      database_id: schedule?.database_id ?? initialDatabaseId ?? "",
      storage_destination_id: schedule?.storage_destination_id ?? "",
      name: schedule?.name ?? "",
      preset: schedule?.preset ?? "every_6_hours",
      cron_expression: schedule?.preset === "custom" ? schedule.cron_expression : "",
      timezone: schedule?.timezone ?? browserTimeZone(),
      compression: schedule?.compression ?? "zstd",
      encryption: schedule?.encryption ?? true,
      verify_after_backup: schedule?.verify_after_backup ?? false,
      enabled: schedule?.enabled ?? true,
      retention: schedule?.retention ?? { daily: 7, weekly: 4, monthly: 6 },
    },
  })
  const errors = form.formState.errors

  // Default the storage destination once destinations load.
  useEffect(() => {
    if (!form.getValues("storage_destination_id") && storage.data?.length) {
      form.setValue("storage_destination_id", (storage.data.find((s) => s.is_default) ?? storage.data[0]).id)
    }
    if (!form.getValues("database_id") && databases.data?.length === 1) form.setValue("database_id", databases.data[0].id)
  }, [storage.data, databases.data, form])

  const [preset, cron, tz, encryption, retention, verifyAfter, enabled] = useWatch({
    control: form.control,
    name: ["preset", "cron_expression", "timezone", "encryption", "retention", "verify_after_backup", "enabled"],
  })
  const debouncedCron = useDebounced(cron, 300)
  const preview = useSchedulePreview(preset, preset === "custom" ? debouncedCron : "", tz, preset !== "custom" || debouncedCron.length > 0)
  const keepAll = !Number(retention.daily) && !Number(retention.weekly) && !Number(retention.monthly)

  if (databases.data?.length === 0 || storage.data?.length === 0) {
    return (
      <>
        <DialogHeader>
          <DialogTitle>Create schedule</DialogTitle>
          <DialogDescription>A schedule needs a database and a storage destination.</DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          {databases.data?.length === 0 && (
            <Alert>
              <AlertDescription>
                No databases yet. <Link href="/databases/new">Add a database</Link> first.
              </AlertDescription>
            </Alert>
          )}
          {storage.data?.length === 0 && (
            <Alert>
              <AlertDescription>
                No storage destinations yet. <Link href="/storage?new=1">Add storage</Link> first.
              </AlertDescription>
            </Alert>
          )}
        </div>
      </>
    )
  }

  const onSubmit = form.handleSubmit((v) => {
    const input = { ...v, name: v.name || undefined, cron_expression: v.preset === "custom" ? v.cron_expression : undefined }
    const handlers = {
      onSuccess: () => {
        toast.success(schedule ? "Schedule updated" : "Schedule created", { description: preview.data?.description })
        onDone()
      },
      onError: (err: unknown) => {
        if (err instanceof ApiError && Object.keys(err.fields).length) {
          for (const [k, message] of Object.entries(err.fields)) form.setError(k as keyof Values, { message })
        } else toast.error("Couldn't save schedule", { description: errorMessage(err) })
      },
    }
    if (schedule) update.mutate(input, handlers)
    else create.mutate(input, handlers)
  })
  const pending = create.isPending || update.isPending

  return (
    <form onSubmit={onSubmit} noValidate className="space-y-6">
      <DialogHeader>
        <DialogTitle>{schedule ? "Edit schedule" : "Create schedule"}</DialogTitle>
        <DialogDescription>The scheduler runs server-side: backups happen whether or not anyone has DBVault open.</DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField id="sc-db" label="Database" error={errors.database_id}>
            <Controller
              control={form.control}
              name="database_id"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger id="sc-db" className="w-full">
                    <SelectValue placeholder="Select a database" />
                  </SelectTrigger>
                  <SelectContent>
                    {databases.data?.map((d) => (
                      <SelectItem key={d.id} value={d.id}>
                        {d.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          </FormField>
          <FormField id="sc-storage" label="Storage destination" error={errors.storage_destination_id}>
            <Controller
              control={form.control}
              name="storage_destination_id"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger id="sc-storage" className="w-full">
                    <SelectValue placeholder="Select storage" />
                  </SelectTrigger>
                  <SelectContent>
                    {storage.data?.map((s) => (
                      <SelectItem key={s.id} value={s.id}>
                        {s.name}
                        {s.is_default ? " (default)" : ""}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          </FormField>
        </div>

        <FieldSet>
          <FieldLegend variant="label">Frequency</FieldLegend>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
            {PRESETS.map((p) => (
              <button
                type="button"
                key={p.value}
                onClick={() => form.setValue("preset", p.value)}
                aria-pressed={preset === p.value}
                className={cn(
                  "rounded-lg border px-3 py-2.5 text-left transition-colors hover:bg-muted/60",
                  preset === p.value && "border-foreground/40 bg-muted/60 ring-1 ring-foreground/20",
                )}
              >
                <span className="block text-sm font-medium">{p.label}</span>
                <span className="block text-xs text-muted-foreground">{p.hint}</span>
              </button>
            ))}
          </div>
        </FieldSet>

        <div className="grid gap-4 sm:grid-cols-2">
          {preset === "custom" && (
            <FormField id="sc-cron" label="Cron expression" description="minute hour day month weekday — e.g. 30 1 * * 1-5 (at most every 5 minutes)" error={errors.cron_expression}>
              <Input id="sc-cron" className="font-mono" placeholder="30 1 * * *" {...form.register("cron_expression")} />
            </FormField>
          )}
          <FormField id="sc-tz" label="Timezone" error={errors.timezone}>
            <Controller
              control={form.control}
              name="timezone"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger id="sc-tz" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent className="max-h-72">
                    {zones.map((z) => (
                      <SelectItem key={z} value={z}>
                        {z}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          </FormField>
        </div>

        <div className="rounded-lg border bg-muted/30 p-3 text-sm">
          {preview.data?.valid ? (
            <div className="space-y-2">
              <div className="flex items-center gap-2 font-medium">
                <CalendarClock className="size-4 text-brand" /> {preview.data.description}
                <code className="ml-auto font-mono text-xs text-muted-foreground">{preview.data.cron_expression}</code>
              </div>
              <ol className="grid gap-1 text-xs text-muted-foreground sm:grid-cols-2">
                {preview.data.next_runs?.map((r, i) => (
                  <li key={r} className="tabular">
                    {i === 0 ? "Next: " : ""}
                    {formatInZone(r, tz)}
                  </li>
                ))}
              </ol>
            </div>
          ) : preview.data && !preview.data.valid ? (
            <p className="flex items-center gap-2 text-destructive">
              <AlertTriangle className="size-4" /> {preview.data.error}
            </p>
          ) : (
            <p className="text-muted-foreground">{preset === "custom" ? "Enter a cron expression to preview run times." : "Loading preview…"}</p>
          )}
        </div>

        <FieldSet>
          <FieldLegend variant="label">Retention</FieldLegend>
          <FieldDescription>
            Grandfather-father-son: keep the newest backup of each of the last N days, weeks and months. The latest backup and anything younger than an hour are always
            kept; manual backups are never deleted automatically.
          </FieldDescription>
          <div className="grid grid-cols-3 gap-3">
            {(["daily", "weekly", "monthly"] as const).map((k) => (
              <FormField key={k} id={`sc-ret-${k}`} label={k.charAt(0).toUpperCase() + k.slice(1)} error={errors.retention?.[k]}>
                <Input id={`sc-ret-${k}`} type="number" min={0} inputMode="numeric" className="tabular" {...form.register(`retention.${k}`)} />
              </FormField>
            ))}
          </div>
          {keepAll && <p className="text-xs text-warning">0 / 0 / 0 keeps every backup forever — storage will grow without limit.</p>}
        </FieldSet>

        <FieldSet>
          <FieldLegend variant="label">Options</FieldLegend>
          <FormField id="sc-compression" label="Compression">
            <Controller
              control={form.control}
              name="compression"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger id="sc-compression" className="w-full sm:w-64">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="zstd">zstd (recommended)</SelectItem>
                    <SelectItem value="gzip">gzip</SelectItem>
                    <SelectItem value="none">None</SelectItem>
                  </SelectContent>
                </Select>
              )}
            />
          </FormField>
          <Field orientation="horizontal">
            <Switch id="sc-enc" checked={encryption} onCheckedChange={(v) => form.setValue("encryption", v)} />
            <FieldContent>
              <FieldLabel htmlFor="sc-enc">Encrypt backups</FieldLabel>
              <FieldDescription>age (X25519 + ChaCha20-Poly1305) with your organization&apos;s key, before upload.</FieldDescription>
              {!encryption && <p className="text-xs text-warning">Unencrypted backups are readable by anyone with access to the bucket.</p>}
            </FieldContent>
          </Field>
          <Field orientation="horizontal">
            <Switch id="sc-verify" checked={verifyAfter} onCheckedChange={(v) => form.setValue("verify_after_backup", v)} />
            <FieldContent>
              <FieldLabel htmlFor="sc-verify">Verify after each backup</FieldLabel>
              <FieldDescription>Restore every new backup into a disposable sandbox database and check its tables.</FieldDescription>
            </FieldContent>
          </Field>
          <Field orientation="horizontal">
            <Switch id="sc-enabled" checked={enabled} onCheckedChange={(v) => form.setValue("enabled", v)} />
            <FieldLabel htmlFor="sc-enabled">Enabled</FieldLabel>
          </Field>
        </FieldSet>
      </FieldGroup>
      <DialogFooter>
        <Button type="submit" disabled={pending}>
          {pending && <Spinner />} {schedule ? "Save changes" : "Create schedule"}
        </Button>
      </DialogFooter>
    </form>
  )
}
