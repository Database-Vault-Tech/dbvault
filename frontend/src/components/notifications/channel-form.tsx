"use client"

import { zodResolver } from "@hookform/resolvers/zod"
import { AlertTriangle, Mail, Webhook } from "lucide-react"
import { useState } from "react"
import { useForm, useWatch } from "react-hook-form"
import { toast } from "sonner"
import { z } from "zod"

import { CopyField } from "@/components/app/copy-button"
import { FormField } from "@/components/app/form-field"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldContent, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { ApiError, errorMessage } from "@/lib/api"
import { useCreateNotification, useNotificationEvents, useUpdateNotification } from "@/lib/queries"
import type { NotificationChannel, NotificationEvent, NotificationInput } from "@/lib/types"

const DEFAULT_EVENTS: NotificationEvent[] = ["backup.failed", "verification.failed", "storage.failed"]

function splitRecipients(raw: string): string[] {
  return raw
    .split(/[\s,;]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

function schema(editing: boolean) {
  return z
    .object({
      name: z.string().trim().min(1, "Give this channel a name.").max(80),
      type: z.enum(["email", "webhook"]),
      recipients: z.string(),
      url: z.string().trim(),
      events: z.array(z.string()).min(1, "Select at least one event."),
      enabled: z.boolean(),
    })
    .superRefine((v, ctx) => {
      if (v.type === "email") {
        const list = splitRecipients(v.recipients)
        if (list.length === 0) ctx.addIssue({ code: "custom", path: ["recipients"], message: "Add at least one email address." })
        const bad = list.find((e) => !z.email().safeParse(e).success)
        if (bad) ctx.addIssue({ code: "custom", path: ["recipients"], message: `${bad} is not a valid email address.` })
      } else if (v.url || !editing) {
        if (!/^https?:\/\/[^\s]+$/.test(v.url)) ctx.addIssue({ code: "custom", path: ["url"], message: "Enter an http(s) URL." })
      }
    })
}
type Values = z.infer<ReturnType<typeof schema>>

export function ChannelDialog({ open, onOpenChange, channel }: { open: boolean; onOpenChange: (o: boolean) => void; channel?: NotificationChannel }) {
  const [secret, setSecret] = useState<string | null>(null)
  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
          {open && (
            <ChannelForm
              channel={channel}
              onDone={(s) => {
                onOpenChange(false)
                if (s) setSecret(s)
              }}
            />
          )}
        </DialogContent>
      </Dialog>
      <Dialog open={!!secret} onOpenChange={(o) => !o && setSecret(null)}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>Webhook signing secret</DialogTitle>
            <DialogDescription>Copy it now — it won&apos;t be shown again.</DialogDescription>
          </DialogHeader>
          {secret && <CopyField value={secret} />}
          <div className="space-y-2 text-sm text-muted-foreground">
            <p>Every request carries these headers. Recompute the HMAC and reject stale timestamps:</p>
            <pre className="overflow-x-auto rounded-lg border bg-zinc-950 p-3 font-mono text-xs text-zinc-100">
              {`X-DBVault-Event: backup.failed
X-DBVault-Timestamp: 1790000000
X-DBVault-Signature: sha256=HMAC_SHA256(secret, timestamp + "." + body)`}
            </pre>
          </div>
          <DialogFooter>
            <Button onClick={() => setSecret(null)}>I&apos;ve saved the secret</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function ChannelForm({ channel, onDone }: { channel?: NotificationChannel; onDone: (secret?: string) => void }) {
  const editing = !!channel
  const events = useNotificationEvents()
  const create = useCreateNotification()
  const update = useUpdateNotification(channel?.id ?? "")
  const form = useForm<Values>({
    resolver: zodResolver(schema(editing)),
    defaultValues: {
      name: channel?.name ?? "",
      type: channel?.type ?? "email",
      recipients: channel?.config.recipients?.join(", ") ?? "",
      url: "",
      events: channel?.events ?? DEFAULT_EVENTS,
      enabled: channel?.enabled ?? true,
    },
  })
  const [type, selected, enabled] = useWatch({ control: form.control, name: ["type", "events", "enabled"] })
  const errors = form.formState.errors

  const onSubmit = form.handleSubmit((v) => {
    const input: NotificationInput = {
      name: v.name,
      type: v.type,
      events: v.events as NotificationEvent[],
      enabled: v.enabled,
      ...(v.type === "email" ? { recipients: splitRecipients(v.recipients) } : v.url ? { url: v.url } : {}),
    }
    const onError = (err: unknown) => {
      if (err instanceof ApiError && Object.keys(err.fields).length) {
        for (const [k, message] of Object.entries(err.fields)) form.setError(k as keyof Values, { message })
      } else toast.error("Couldn't save channel", { description: errorMessage(err) })
    }
    if (editing) {
      update.mutate(input, {
        onSuccess: () => {
          toast.success("Channel updated")
          onDone()
        },
        onError,
      })
    } else {
      create.mutate(input, {
        onSuccess: (r) => {
          toast.success(`Created ${r.notification.name}`)
          onDone(r.signing_secret)
        },
        onError,
      })
    }
  })
  const pending = create.isPending || update.isPending

  return (
    <form onSubmit={onSubmit} noValidate className="space-y-5">
      <DialogHeader>
        <DialogTitle>{editing ? `Edit ${channel.name}` : "Add notification channel"}</DialogTitle>
        <DialogDescription>Get told when backups fail — before you need them. Slack and Discord are on the roadmap; webhooks work with any HTTP endpoint.</DialogDescription>
      </DialogHeader>
      <FieldGroup>
        {!editing && (
          <Tabs value={type} onValueChange={(v) => form.setValue("type", v as Values["type"])}>
            <TabsList className="w-full">
              <TabsTrigger value="email">
                <Mail /> Email
              </TabsTrigger>
              <TabsTrigger value="webhook">
                <Webhook /> Webhook
              </TabsTrigger>
            </TabsList>
          </Tabs>
        )}
        {type === "email" && events.data && !events.data.email_configured && (
          <Alert>
            <AlertTriangle />
            <AlertTitle>Email isn&apos;t configured on this server</AlertTitle>
            <AlertDescription>
              Set SMTP_HOST and SMTP_FROM for the API and worker. For local development, docker compose includes Mailpit — view messages at http://localhost:8025.
            </AlertDescription>
          </Alert>
        )}
        <FormField id="nc-name" label="Name" error={errors.name}>
          <Input id="nc-name" placeholder={type === "email" ? "On-call team" : "Incident webhook"} {...form.register("name")} />
        </FormField>
        {type === "email" ? (
          <FormField id="nc-rcpt" label="Recipients" description="Separate addresses with commas or new lines (max 20)." error={errors.recipients}>
            <Textarea id="nc-rcpt" rows={2} placeholder="oncall@example.com, dba@example.com" {...form.register("recipients")} />
          </FormField>
        ) : (
          <FormField
            id="nc-url"
            label="Webhook URL"
            description={editing ? `Currently ${channel.config.url_hint ?? "set"}. Leave blank to keep the stored URL (it's encrypted at rest).` : "Receives a signed JSON POST per event. Stored encrypted."}
            error={errors.url}
          >
            <Input id="nc-url" className="font-mono" placeholder="https://hooks.example.com/dbvault" {...form.register("url")} />
          </FormField>
        )}
        <FieldSet>
          <FieldLegend variant="label">Events</FieldLegend>
          <div className="space-y-2.5">
            {events.data?.events.map((e) => (
              <Field key={e.type} orientation="horizontal">
                <Checkbox
                  id={`ev-${e.type}`}
                  checked={selected.includes(e.type)}
                  onCheckedChange={(c) =>
                    form.setValue("events", c ? [...selected, e.type] : selected.filter((x) => x !== e.type), { shouldValidate: form.formState.isSubmitted })
                  }
                />
                <FieldContent>
                  <FieldLabel htmlFor={`ev-${e.type}`}>{e.label}</FieldLabel>
                  <FieldDescription>{e.description}</FieldDescription>
                </FieldContent>
              </Field>
            ))}
          </div>
          <FieldError errors={errors.events ? [errors.events] : undefined} />
        </FieldSet>
        <Field orientation="horizontal">
          <Switch id="nc-enabled" checked={enabled} onCheckedChange={(v) => form.setValue("enabled", v)} />
          <FieldLabel htmlFor="nc-enabled">Enabled</FieldLabel>
        </Field>
      </FieldGroup>
      <DialogFooter>
        <Button type="submit" disabled={pending}>
          {pending && <Spinner />} {editing ? "Save changes" : "Add channel"}
        </Button>
      </DialogFooter>
    </form>
  )
}
