"use client"

import { AlertTriangle, ArrowLeft, Download, EyeOff, KeyRound, Search, Sparkles, Table2 } from "lucide-react"
import Link from "next/link"
import { useMemo, useState } from "react"
import { toast } from "sonner"

import { EmptyState } from "@/components/app/empty-state"
import { ErrorState } from "@/components/app/error-state"
import { PageHeader } from "@/components/app/page-header"
import { RelativeTime } from "@/components/app/relative-time"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { ApiError, errorMessage } from "@/lib/api"
import { allowedRules, applySuggestions, columnRule, isTruncated, ruleName, RULES, setColumnRule, setTruncate, toYAML, unruledPersonal } from "@/lib/masking"
import { useOrg } from "@/lib/org"
import { useDatabase, useMaskingEditor, useSaveMaskingProfile } from "@/lib/queries"
import type { CatalogColumn, CatalogTable, ColumnRuleValue, MaskingEditor as Editor, MaskingRule, MaskingRules } from "@/lib/types"
import { cn } from "@/lib/utils"

const PROFILE = "default"
const COPY = "__copy__" // select value for "no rule": copied unchanged

export function MaskingEditor({ id }: { id: string }) {
  const db = useDatabase(id)
  const editor = useMaskingEditor(id)
  const name = db.data?.database.name

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <Button variant="ghost" size="sm" asChild className="-ml-2 text-muted-foreground">
        <Link href={`/databases/${id}`}>
          <ArrowLeft /> {name ?? "Database"}
        </Link>
      </Button>
      <PageHeader
        title="Data masking"
        description="Rules for anonymized restores: DBVault restores a backup into a sandbox, replaces personal data with realistic fakes, and hands only the masked copy to staging."
      />
      {editor.error ? (
        <ErrorState error={editor.error} retry={() => editor.refetch()} />
      ) : editor.isPending ? (
        <div className="space-y-4">
          <Skeleton className="h-24" />
          <Skeleton className="h-64" />
        </div>
      ) : !editor.data.supported ? (
        <Alert>
          <AlertTriangle />
          <AlertTitle>Not available for this database yet</AlertTitle>
          <AlertDescription>{editor.data.unsupported_reason}</AlertDescription>
        </Alert>
      ) : !editor.data.schema ? (
        <Card>
          <CardContent className="py-4">
            <EmptyState
              icon={Table2}
              title="Verify a backup first"
              description="DBVault learns this database's tables and columns (never its data) when a backup is restore-tested. Verify any backup of it, then come back to set up masking."
              action={
                <Button asChild>
                  <Link href={`/databases/${id}`}>View its backups</Link>
                </Button>
              }
            />
          </CardContent>
        </Card>
      ) : (
        <RulesEditor key={editor.dataUpdatedAt} databaseId={id} editor={editor.data} />
      )}
    </div>
  )
}

function RulesEditor({ databaseId, editor }: { databaseId: string; editor: Editor }) {
  const { can } = useOrg()
  const canEdit = can("admin")
  const saved = editor.profiles.find((p) => p.name === PROFILE)
  const tables = editor.schema!.tables
  const [rules, setRules] = useState<MaskingRules>(saved?.rules ?? editor.suggested ?? { tables: {} })
  const [dirty, setDirty] = useState(!saved)
  const [serverProblems, setServerProblems] = useState<string[]>(editor.problems[PROFILE] ?? [])
  const [search, setSearch] = useState("")
  const [showAll, setShowAll] = useState(false)
  const save = useSaveMaskingProfile(databaseId)

  const update = (next: MaskingRules) => {
    setRules(next)
    setDirty(true)
  }
  const unruled = useMemo(() => unruledPersonal(rules, tables), [rules, tables])
  const personal = tables.reduce((n, t) => n + t.columns.filter((c) => c.personal).length, 0)
  const truncated = Object.values(rules.tables).filter((t) => t === "truncate").length
  const ruled = Object.values(rules.tables).reduce((n, t) => n + (t === "truncate" ? 0 : Object.keys(t).length), 0)
  const visible = tables.filter((t) => !search || t.key.toLowerCase().includes(search.toLowerCase()) || t.columns.some((c) => c.name.toLowerCase().includes(search.toLowerCase())))

  const onSave = () =>
    save.mutate(
      { name: PROFILE, rules },
      {
        onSuccess: (r) => {
          setDirty(false)
          setServerProblems(r.problems)
          toast.success(`Saved masking profile (version ${r.profile.version})`, {
            description: r.problems.length ? "A masked restore would stop until the listed problems are fixed." : "Masked restores will use these rules.",
          })
        },
        onError: (err) => toast.error(err instanceof ApiError && err.fields.rules ? err.fields.rules : errorMessage(err)),
      },
    )
  const exportYAML = () => {
    const url = URL.createObjectURL(new Blob([toYAML(rules)], { type: "text/yaml" }))
    const a = document.createElement("a")
    a.href = url
    a.download = "masking.yaml"
    a.click()
    URL.revokeObjectURL(url)
  }
  const problems = dirty ? unruled.map((c) => `${c} looks like personal data but has no rule`) : serverProblems

  return (
    <div className="space-y-6">
      <Card>
        <CardContent className="flex flex-wrap items-center gap-x-8 gap-y-4">
          <Stat label="Personal columns found" value={personal} />
          <Stat label="Columns with rules" value={ruled} />
          <Stat label="Tables emptied" value={truncated} />
          <Stat label="Undecided" value={unruled.length} warn={unruled.length > 0} />
          <div className="min-w-0 flex-1 text-xs text-muted-foreground sm:text-right">
            {saved ? (
              <>
                Profile <span className="font-mono">{PROFILE}</span> v{saved.version}
                {saved.updated_by_email && ` · ${saved.updated_by_email}`} · <RelativeTime date={saved.updated_at} />
              </>
            ) : (
              "Not saved yet: these are DBVault's suggestions."
            )}
            {editor.schema_source && (
              <div>
                Schema from the backup of <RelativeTime date={editor.schema_source.backup_created_at} />
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {problems.length > 0 && (
        <Alert className="border-warning/40 bg-warning/5">
          <AlertTriangle className="text-warning" />
          <AlertTitle>A masked restore would stop on {problems.length === 1 ? "this" : `these ${problems.length} problems`}</AlertTitle>
          <AlertDescription>
            <ul className="mt-1 list-disc space-y-0.5 pl-4">
              {problems.slice(0, 8).map((p) => (
                <li key={p}>{p}</li>
              ))}
              {problems.length > 8 && <li>…and {problems.length - 8} more</li>}
            </ul>
            <p className="mt-2">Give each one a rule, or choose “Keep (reviewed)” if it isn&apos;t personal. This protects you when a migration adds a new column.</p>
          </AlertDescription>
        </Alert>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <div className="relative w-full sm:w-72">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search tables and columns…" className="pl-8" aria-label="Search tables and columns" />
        </div>
        <div className="flex items-center gap-2">
          <Switch id="show-all" checked={showAll} onCheckedChange={setShowAll} />
          <Label htmlFor="show-all" className="text-sm font-normal">
            Show every column
          </Label>
        </div>
        <div className="ml-auto flex flex-wrap gap-2">
          {canEdit && editor.suggested && (
            <Button variant="outline" size="sm" onClick={() => update(applySuggestions(rules, editor.suggested!))}>
              <Sparkles /> Apply suggestions
            </Button>
          )}
          <Button variant="outline" size="sm" onClick={exportYAML}>
            <Download /> Export YAML
          </Button>
          {canEdit && (
            <Button size="sm" onClick={onSave} disabled={save.isPending || (!dirty && !!saved)}>
              {save.isPending && <Spinner />} {saved ? "Save changes" : "Save profile"}
            </Button>
          )}
        </div>
      </div>

      <div className="space-y-4">
        {visible.map((t) => (
          <TableCard key={t.key} table={t} rules={rules} examples={editor.examples} showAll={showAll || !!search} readOnly={!canEdit} onChange={update} />
        ))}
      </div>
    </div>
  )
}

function Stat({ label, value, warn }: { label: string; value: number; warn?: boolean }) {
  return (
    <div>
      <div className={cn("text-2xl font-semibold tabular-nums", warn && "text-warning")}>{value}</div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  )
}

function TableCard({
  table,
  rules,
  examples,
  showAll,
  readOnly,
  onChange,
}: {
  table: CatalogTable
  rules: MaskingRules
  examples: Record<string, string>
  showAll: boolean
  readOnly: boolean
  onChange: (r: MaskingRules) => void
}) {
  const truncated = isTruncated(rules, table.key)
  const personal = table.columns.filter((c) => c.personal).length
  const shown = table.columns.filter((c) => showAll || c.personal || columnRule(rules, table.key, c.name) !== undefined)
  const hidden = table.columns.length - shown.length

  return (
    <Card size="sm">
      <CardHeader className="flex flex-row flex-wrap items-center gap-3">
        <span className="font-mono text-sm font-medium">{table.key}</span>
        {personal > 0 && <Badge variant="outline">{personal} personal</Badge>}
        <div className="ml-auto">
          <Select value={truncated ? "truncate" : "mask"} onValueChange={(v) => onChange(setTruncate(rules, table.key, v === "truncate"))} disabled={readOnly}>
            <SelectTrigger size="sm" className="w-52" aria-label={`What to do with ${table.key}`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="mask">Mask columns</SelectItem>
              <SelectItem value="truncate">Empty the table</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </CardHeader>
      <CardContent>
        {truncated ? (
          <p className="text-sm text-muted-foreground">
            All rows are removed; the table itself stays. {table.suggest_truncate ? "Good for sessions, logs and events." : ""}
          </p>
        ) : shown.length === 0 ? (
          <p className="text-sm text-muted-foreground">No personal-looking columns. {hidden} columns are copied unchanged.</p>
        ) : (
          <div className="divide-y rounded-lg border">
            {shown.map((c) => (
              <ColumnRow key={c.name} table={table.key} column={c} rules={rules} examples={examples} readOnly={readOnly} onChange={onChange} />
            ))}
            {hidden > 0 && <div className="px-3 py-2 text-xs text-muted-foreground">{hidden} other columns are copied unchanged.</div>}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function ColumnRow({
  table,
  column,
  rules,
  examples,
  readOnly,
  onChange,
}: {
  table: string
  column: CatalogColumn
  rules: MaskingRules
  examples: Record<string, string>
  readOnly: boolean
  onChange: (r: MaskingRules) => void
}) {
  const value = columnRule(rules, table, column.name)
  const rule = ruleName(value)
  const allowed = allowedRules(column)
  const undecided = column.personal && rule === undefined
  const set = (v: ColumnRuleValue | undefined) => onChange(setColumnRule(rules, table, column.name, v))

  return (
    <div className={cn("grid items-center gap-3 px-3 py-2.5 sm:grid-cols-[minmax(0,1fr)_14rem_minmax(0,1.2fr)]", undecided && "bg-warning/5")}>
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="truncate font-mono text-sm">{column.name}</span>
          {column.personal && (
            <Badge variant="outline" className="border-warning/40 text-warning">
              <EyeOff className="size-3" /> personal
            </Badge>
          )}
          {column.key && (
            <Badge variant="outline">
              <KeyRound className="size-3" /> key
            </Badge>
          )}
          {column.unique && !column.key && <Badge variant="outline">unique</Badge>}
        </div>
        <div className="font-mono text-xs text-muted-foreground">
          {column.type}
          {!column.nullable && " · not null"}
        </div>
      </div>
      <Select
        value={rule ?? COPY}
        disabled={readOnly || (column.key && rule === undefined)}
        onValueChange={(v) => set(v === COPY ? undefined : v === "redact" ? { redact: "REDACTED" } : (v as MaskingRule))}
      >
        <SelectTrigger size="sm" className="w-full" aria-label={`Rule for ${table}.${column.name}`}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={COPY}>{column.personal ? "Undecided" : "Copy unchanged"}</SelectItem>
          {RULES.filter((r) => allowed.includes(r.id)).map((r) => (
            <SelectItem key={r.id} value={r.id}>
              {r.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <div className="min-w-0 text-xs text-muted-foreground">
        {value && typeof value !== "string" ? (
          <Input
            value={value.redact}
            onChange={(e) => set({ redact: e.target.value })}
            disabled={readOnly}
            className="h-8 font-mono text-xs"
            aria-label={`Replacement value for ${table}.${column.name}`}
            maxLength={column.max_length || 1024}
          />
        ) : rule ? (
          <span className="block truncate font-mono" title={examples[rule]}>
            {examples[rule]}
          </span>
        ) : column.key ? (
          "Key columns are copied so relationships keep working."
        ) : column.personal ? (
          <span className="text-warning">Suggested: {RULES.find((r) => r.id === column.suggested)?.label ?? column.suggested}</span>
        ) : null}
      </div>
    </div>
  )
}
