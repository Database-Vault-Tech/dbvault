"use client"

import { AlertTriangle, ArrowDown, Check, DatabaseZap, RotateCcw } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"

import { ConfirmDialog } from "@/components/app/confirm-dialog"
import { StatusBadge } from "@/components/app/status"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { ApiError, errorMessage } from "@/lib/api"
import { databaseLocation, engineMeta } from "@/lib/engines"
import { formatBytes, formatDateTime, formatRelative } from "@/lib/format"
import { useBackup, useBackups, useCreateRestore, useDatabases } from "@/lib/queries"
import { isSQLitePath } from "@/lib/schemas"
import { cn } from "@/lib/utils"

const IDENT = /^[a-zA-Z_][a-zA-Z0-9_-]{0,62}$/

function yyyymmdd(d = new Date()) {
  return `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, "0")}${String(d.getDate()).padStart(2, "0")}`
}

function suggestName(database: string, fileBased: boolean) {
  if (fileBased) {
    // app/shop.db → app/shop-restored-20260926.db, next to the original.
    const m = /^(.*?)(\.[A-Za-z0-9]+)?$/.exec(database)
    return `${m?.[1] ?? database}-restored-${yyyymmdd()}${m?.[2] ?? ""}`
  }
  const base = database.replace(/[^a-zA-Z0-9_]/g, "_").replace(/^[^a-zA-Z_]/, "_$&")
  return `${base}_restored_${yyyymmdd()}`.slice(0, 63)
}

function Step({ n, title, children, disabled }: { n: number; title: string; children: React.ReactNode; disabled?: boolean }) {
  return (
    <div className={cn("grid gap-3 sm:grid-cols-[28px_1fr]", disabled && "pointer-events-none opacity-50")}>
      <span className="flex size-6 items-center justify-center rounded-full border text-xs font-medium text-muted-foreground">{n}</span>
      <div className="min-w-0 space-y-3">
        <div className="text-sm font-medium">{title}</div>
        {children}
      </div>
    </div>
  )
}

/**
 * The restore form, in a dialog the same size as the restore progress
 * dialog it hands over to.
 */
export function RestoreWizardDialog({
  open,
  onOpenChange,
  initialBackupId,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initialBackupId?: string
  onCreated: (id: string) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl">
        <RestoreWizard initialBackupId={initialBackupId} onCreated={onCreated} onCancel={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  )
}

export function RestoreWizard({
  initialBackupId,
  onCreated,
  onCancel,
}: {
  initialBackupId?: string
  onCreated: (id: string) => void
  onCancel: () => void
}) {
  const { data: databases } = useDatabases()
  const preset = useBackup(initialBackupId ?? "")
  const [sourceId, setSourceId] = useState("")
  const [backupId, setBackupId] = useState("")
  const [targetId, setTargetId] = useState("")
  const [mode, setMode] = useState<"new" | "existing">("new")
  const [nameInput, setNameInput] = useState<string | null>(null)
  const [confirming, setConfirming] = useState(false)
  const create = useCreateRestore()

  // Preselect from ?backup= (state adjusted during render, once).
  const presetBackup = preset.data?.backup
  const [appliedPreset, setAppliedPreset] = useState<string | null>(null)
  if (presetBackup && appliedPreset !== presetBackup.id && !sourceId) {
    setAppliedPreset(presetBackup.id)
    setSourceId(presetBackup.database_id)
    setBackupId(presetBackup.id)
    setTargetId(presetBackup.database_id)
  }

  const backups = useBackups({ database_id: sourceId || undefined, status: "completed" })
  const backupList = useMemo(() => (sourceId ? (backups.data?.pages.flatMap((p) => p.data) ?? []) : []), [backups.data, sourceId])
  const source = databases?.find((d) => d.id === sourceId)
  const backup = backupList.find((b) => b.id === backupId) ?? (presetBackup?.id === backupId ? presetBackup : undefined)
  // A backup can only be restored into a server of the same engine.
  const engine = engineMeta(backup?.engine ?? source?.engine)
  const targets = databases?.filter((d) => engineMeta(d.engine).id === engine.id)
  const target = targets?.find((d) => d.id === targetId)
  const createPrivilege = engine.id === "postgres" ? "CREATEDB privilege" : "CREATE privilege"
  // What "new" creates, and what it needs, in the engine's own terms.
  const newThing = engine.fileBased ? "file" : "database"
  const createNeeds = engine.fileBased
    ? "DBVault needs write access to the folder the file goes in."
    : `The database user needs the ${createPrivilege} on the target server.`
  // Suggest a name until the user types their own.
  const newName = nameInput ?? (target ? suggestName(target.database, engine.fileBased) : "")
  const nameValid = mode === "existing" || (engine.fileBased ? isSQLitePath(newName) : IDENT.test(newName))
  // Only flag the name as invalid once there is something to judge; an empty
  // field before a target is chosen is not an error yet.
  const showNameError = mode === "new" && newName !== "" && !nameValid
  // Typing the target's own name means the user wants to overwrite it.
  const nameIsTarget = mode === "new" && !!target && newName.trim().toLowerCase() === target.database.toLowerCase()
  // The target must be a live database (a backup's source may have been removed).
  const ready = !!backupId && !!target && nameValid && !nameIsTarget

  const selectSource = (id: string) => {
    setSourceId(id)
    setBackupId("")
    setTargetId(id)
    setNameInput(null)
  }

  const submit = () =>
    create.mutate(
      { backup_id: backupId, target_database_id: targetId, mode, new_database_name: mode === "new" ? newName : undefined, confirmation: mode === "existing" ? "RESTORE" : undefined },
      {
        onSuccess: (r) => {
          toast.success("Restore queued", { description: `Restoring into ${mode === "new" ? newName : target?.name}` })
          setConfirming(false)
          onCreated(r.id)
        },
        onError: (err) => toast.error("Couldn't start restore", {
          description: err instanceof ApiError && Object.keys(err.fields).length ? Object.values(err.fields).join(" ") : errorMessage(err),
        }),
      },
    )

  return (
    <>
      <div className="flex items-start gap-3 border-b px-6 py-5 pr-12">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-brand/10 text-brand">
          <RotateCcw className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <DialogTitle className="text-lg">New restore</DialogTitle>
          <DialogDescription className="mt-0.5">
            The backup is downloaded, its checksum verified, then decrypted and restored with {engine.restoreTool}.
          </DialogDescription>
        </div>
      </div>
      <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-6 py-5">
        <Step n={1} title="Source database">
          <Select value={sourceId} onValueChange={selectSource}>
            <SelectTrigger className="w-full sm:w-80" aria-label="Source database">
              <SelectValue placeholder="Select the database that was backed up" />
            </SelectTrigger>
            <SelectContent>
              {databases?.map((d) => (
                <SelectItem key={d.id} value={d.id}>
                  {d.name} <span className="text-muted-foreground">({d.backup_count} backups)</span>
                </SelectItem>
              ))}
              {presetBackup && sourceId === presetBackup.database_id && !databases?.some((d) => d.id === sourceId) && (
                <SelectItem value={presetBackup.database_id}>
                  {presetBackup.database_name} <span className="text-muted-foreground">(removed)</span>
                </SelectItem>
              )}
            </SelectContent>
          </Select>
        </Step>

        <Step n={2} title="Backup" disabled={!sourceId}>
          {sourceId && backups.isPending ? (
            <Spinner />
          ) : sourceId && backupList.length === 0 ? (
            <p className="text-sm text-muted-foreground">{source?.name} has no completed backups yet.</p>
          ) : (
            <RadioGroup value={backupId} onValueChange={setBackupId} className="max-h-72 gap-0 overflow-auto rounded-lg border">
              {backupList.map((b) => (
                <Label
                  key={b.id}
                  htmlFor={`b-${b.id}`}
                  className={cn("flex cursor-pointer items-center gap-3 border-b px-3 py-2.5 font-normal last:border-b-0 hover:bg-muted/50", backupId === b.id && "bg-muted/60")}
                >
                  <RadioGroupItem id={`b-${b.id}`} value={b.id} />
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm">{formatDateTime(b.created_at)}</span>
                    <span className="block text-xs text-muted-foreground">
                      {formatRelative(b.created_at)} · {b.trigger} · {b.storage_name}
                    </span>
                  </span>
                  {b.verification_status === "passed" && <StatusBadge status="passed" label="Verified" />}
                  <span className="text-sm tabular text-muted-foreground">{formatBytes(b.size_bytes)}</span>
                </Label>
              ))}
            </RadioGroup>
          )}
          {backups.hasNextPage && (
            <Button variant="ghost" size="sm" onClick={() => backups.fetchNextPage()}>
              Load older backups
            </Button>
          )}
        </Step>

        <Step n={3} title="Destination" disabled={!backupId}>
          <Field className="sm:max-w-80">
            <FieldLabel>{engine.fileBased ? "Target database" : "Target database server"}</FieldLabel>
            <Select value={targetId} onValueChange={setTargetId}>
              <SelectTrigger className="w-full" aria-label="Target database">
                <SelectValue placeholder="Select target" />
              </SelectTrigger>
              <SelectContent>
                {targets?.map((d) => (
                  <SelectItem key={d.id} value={d.id}>
                    {d.name} <span className="font-mono text-xs text-muted-foreground">{databaseLocation(d, false)}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <RadioGroup value={mode} onValueChange={(v) => setMode(v as "new" | "existing")} className="grid gap-3 md:grid-cols-2">
            <Label htmlFor="mode-new" className={cn("flex cursor-pointer items-start gap-3 rounded-lg border p-3 font-normal", mode === "new" && "border-brand ring-1 ring-brand/30")}>
              <RadioGroupItem id="mode-new" value="new" className="mt-0.5" />
              <span>
                <span className="flex items-center gap-1.5 text-sm font-medium">
                  <DatabaseZap className="size-4" /> Restore into a new {newThing}
                </span>
                <span className="block text-xs text-muted-foreground">
                  {engine.fileBased
                    ? "Safe: writes a new file in the SQLite folder. Existing files are untouched."
                    : "Safe: creates a fresh database on the target server. Existing data is untouched."}
                </span>
              </span>
            </Label>
            <Label htmlFor="mode-existing" className={cn("flex cursor-pointer items-start gap-3 rounded-lg border p-3 font-normal", mode === "existing" && "border-destructive ring-1 ring-destructive/30")}>
              <RadioGroupItem id="mode-existing" value="existing" className="mt-0.5" />
              <span>
                <span className="flex items-center gap-1.5 text-sm font-medium">
                  <AlertTriangle className="size-4 text-destructive" /> Restore into the existing {newThing}
                </span>
                <span className="block text-xs text-muted-foreground">
                  Destructive: {engine.fileBased ? "replaces" : "replaces objects in"} {target?.database ?? `the target ${newThing}`}.
                  {!engine.atomicRestore && " Not transactional."}
                </span>
              </span>
            </Label>
          </RadioGroup>
          {mode === "new" ? (
            <Field data-invalid={showNameError} className="sm:max-w-80">
              <FieldLabel htmlFor="new-name">{engine.fileBased ? "New file path" : "New database name"}</FieldLabel>
              <Input id="new-name" className="font-mono" value={newName} onChange={(e) => setNameInput(e.target.value)} />
              {nameIsTarget ? (
                <Alert className="mt-1 border-warning/40 bg-warning/5">
                  <AlertTriangle className="text-warning" />
                  <AlertTitle>
                    {target?.database} already exists {engine.fileBased ? "in the SQLite folder" : "on this server"}
                  </AlertTitle>
                  <AlertDescription>
                    <p>
                      A new {newThing} needs a new name. To put the backup into {target?.database} itself, restore into the existing {newThing}.
                    </p>
                    <Button type="button" size="sm" variant="outline" className="mt-2" onClick={() => setMode("existing")}>
                      Restore into {target?.database} instead
                    </Button>
                  </AlertDescription>
                </Alert>
              ) : !showNameError ? (
                <FieldDescription>{createNeeds}</FieldDescription>
              ) : engine.fileBased ? (
                <FieldError>Use a path inside the SQLite folder, like app/restored.db (no leading /, no ..).</FieldError>
              ) : (
                <FieldError>Start with a letter or underscore; use letters, numbers, _ or - (max 63).</FieldError>
              )}
            </Field>
          ) : (
            <Alert variant="destructive">
              <AlertTriangle />
              <AlertTitle>Restoring this backup may overwrite existing data.</AlertTitle>
              <AlertDescription>
                {engine.fileBased ? (
                  <>
                    <span className="font-mono">{target?.database}</span> is replaced by the backup. Stop the application using it first: DBVault refuses to
                    replace a file that&apos;s in use.
                  </>
                ) : (
                  <>
                    Every table, view and function contained in the backup is dropped and recreated in <span className="font-mono">{target?.database}</span>.
                  </>
                )}{" "}
                {engine.fileBased
                  ? "The new file is written and checked first, then swapped in: if anything fails, the original is left unchanged."
                  : engine.atomicRestore
                  ? "The restore runs in a single transaction, so it is all-or-nothing: if anything fails, the database is left unchanged."
                  : `${engine.label} can't restore in a single transaction: if the restore fails part-way, the database is left partially restored. Restoring into a new database first is safer.`}{" "}
                Data written after the backup was taken will be lost.
              </AlertDescription>
            </Alert>
          )}
        </Step>

      </div>
      <div className="flex flex-wrap items-center justify-end gap-2 border-t bg-muted/30 px-6 py-4">
        <Button variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button variant={mode === "existing" ? "destructive" : "default"} disabled={!ready || create.isPending} onClick={() => setConfirming(true)}>
          <RotateCcw /> Review and restore
        </Button>
      </div>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        destructive={mode === "existing"}
        phrase={mode === "existing" ? "RESTORE" : undefined}
        pending={create.isPending}
        title={mode === "existing" ? `Overwrite ${target?.name}?` : "Start restore?"}
        confirmLabel={mode === "existing" ? "Restore and overwrite" : "Start restore"}
        onConfirm={submit}
        className="data-[size=default]:sm:max-w-lg"
        icon={
          mode === "existing" ? undefined : (
            <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-brand/10 text-brand">
              <RotateCcw className="size-4" />
            </span>
          )
        }
        description={
          mode === "existing"
            ? engine.fileBased
              ? "The target file will be replaced by the backup. Stop the application using it first."
              : "Every object contained in the backup will be dropped and recreated in the target database."
            : `DBVault will create a new ${newThing} and restore the backup into it. Existing data is not touched.`
        }
      >
        <div className="min-w-0 space-y-3">
          <div className="grid gap-2">
            <SummaryRow
              label="From backup"
              title={source?.name ?? backup?.database_name ?? "—"}
              detail={backup ? `${formatDateTime(backup.created_at)} · ${formatBytes(backup.size_bytes)}` : "—"}
            />
            <div className="flex justify-center text-muted-foreground" aria-hidden>
              <ArrowDown className="size-4" />
            </div>
            <SummaryRow
              label={mode === "new" ? `Into new ${newThing}` : `Over existing ${newThing}`}
              title={mode === "new" ? newName : (target?.database ?? "—")}
              detail={target ? `${target.name} · ${engine.fileBased ? "SQLite folder" : `${target.host}:${target.port}`}` : "—"}
              tone={mode === "existing" ? "danger" : "brand"}
            />
          </div>
          <ul className="space-y-1.5 text-sm text-muted-foreground">
            <SummaryStep>Checksum verified before anything is restored</SummaryStep>
            {engine.fileBased ? (
              <SummaryStep>Written to a temporary file, integrity-checked, then swapped in: all-or-nothing</SummaryStep>
            ) : engine.atomicRestore ? (
              <SummaryStep>Restored in a single transaction: all-or-nothing</SummaryStep>
            ) : (
              <SummaryStep warn={mode === "existing"}>Not transactional: a failed restore can leave the target partially restored</SummaryStep>
            )}
            {mode === "new" ? (
              <SummaryStep>{engine.fileBased ? "Needs write access to the SQLite folder" : `Needs the ${createPrivilege} on the target server`}</SummaryStep>
            ) : (
              <SummaryStep warn>Data written after this backup was taken will be lost</SummaryStep>
            )}
          </ul>
        </div>
      </ConfirmDialog>
    </>
  )
}

function SummaryRow({ label, title, detail, tone = "neutral" }: { label: string; title: string; detail: string; tone?: "neutral" | "brand" | "danger" }) {
  return (
    <div
      className={cn(
        "min-w-0 rounded-lg border px-3 py-2.5",
        tone === "brand" && "border-brand/40 bg-brand/5",
        tone === "danger" && "border-destructive/40 bg-destructive/5",
      )}
    >
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="truncate font-mono text-sm font-medium text-foreground" title={title}>
        {title}
      </div>
      <div className="truncate text-xs text-muted-foreground" title={detail}>
        {detail}
      </div>
    </div>
  )
}

function SummaryStep({ children, warn }: { children: React.ReactNode; warn?: boolean }) {
  return (
    <li className="flex items-start gap-2">
      {warn ? <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-destructive" /> : <Check className="mt-0.5 size-3.5 shrink-0 text-brand" />}
      <span className={cn(warn && "text-destructive")}>{children}</span>
    </li>
  )
}
