"use client"

import { AlertTriangle, Download, ShieldCheck } from "lucide-react"
import { QRCodeSVG } from "qrcode.react"
import { type FormEvent, useState } from "react"
import { toast } from "sonner"

import { CopyButton, CopyField } from "@/components/app/copy-button"
import { ErrorState } from "@/components/app/error-state"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { ApiError, errorMessage } from "@/lib/api"
import { useDisableTwoFactor, useEnableTwoFactor, useRegenerateRecoveryCodes, useSetupTwoFactor, useTwoFactor } from "@/lib/queries"
import type { TotpSetup } from "@/lib/types"

type Dialogs = "enable" | "disable" | "regenerate" | null

export function TwoFactorCard() {
  const status = useTwoFactor()
  const [open, setOpenState] = useState<Dialogs>(null)
  // Remounting on every open gives each dialog fresh state, without clearing
  // it on close (which would flash the first step during the exit animation).
  const [instance, setInstance] = useState(0)
  const setOpen = (d: Dialogs) => {
    setInstance((i) => i + 1)
    setOpenState(d)
  }
  const st = status.data
  const close = () => setOpenState(null)
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          Two-factor authentication
          {st && <Badge variant={st.enabled ? "secondary" : "outline"}>{st.enabled ? "On" : "Off"}</Badge>}
        </CardTitle>
        <CardDescription>
          Require a code from an authenticator app (1Password, Google Authenticator, Authy…) when you sign in, so a stolen password alone can&apos;t
          reach your databases and backups.
        </CardDescription>
        {st && (
          <CardAction className="flex flex-wrap justify-end gap-2">
            {st.enabled ? (
              <>
                <Button variant="outline" size="sm" onClick={() => setOpen("regenerate")}>
                  New recovery codes
                </Button>
                <Button variant="outline" size="sm" onClick={() => setOpen("disable")}>
                  Turn off
                </Button>
              </>
            ) : (
              <Button variant="outline" size="sm" onClick={() => setOpen("enable")}>
                <ShieldCheck /> Set up
              </Button>
            )}
          </CardAction>
        )}
      </CardHeader>
      {(status.error || status.isPending || st?.enabled) && (
        <CardContent>
          {status.error ? (
            <ErrorState error={status.error} retry={() => status.refetch()} />
          ) : status.isPending ? (
            <Skeleton className="h-5 w-64" />
          ) : st?.recovery_codes_remaining ? (
            <p className={st.recovery_codes_remaining <= 3 ? "text-sm text-warning" : "text-sm text-muted-foreground"}>
              {st.recovery_codes_remaining} of 10 recovery codes left.
              {st.recovery_codes_remaining <= 3 && " Generate new ones before you run out."}
            </p>
          ) : (
            <Alert variant="destructive">
              <AlertTriangle />
              <AlertTitle>No recovery codes left</AlertTitle>
              <AlertDescription>If you lose your authenticator you&apos;ll be locked out. Generate new recovery codes now.</AlertDescription>
            </Alert>
          )}
        </CardContent>
      )}
      <EnableDialog key={`enable-${instance}`} open={open === "enable"} onClose={close} />
      <DisableDialog key={`disable-${instance}`} open={open === "disable"} onClose={close} />
      <RegenerateDialog key={`regenerate-${instance}`} open={open === "regenerate"} onClose={close} />
    </Card>
  )
}

/** Maps API field errors onto the form, falling back to a toast. */
function handleError(err: unknown, setFields: (f: Record<string, string>) => void) {
  if (err instanceof ApiError && Object.keys(err.fields).length) setFields(err.fields)
  else toast.error(errorMessage(err))
}

function EnableDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const setup = useSetupTwoFactor()
  const enable = useEnableTwoFactor()
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [fields, setFields] = useState<Record<string, string>>({})
  const [secret, setSecret] = useState<TotpSetup | null>(null)
  const [codes, setCodes] = useState<string[] | null>(null)

  const close = () => {
    // Recovery codes must be acknowledged with the Done button.
    if (!codes) onClose()
  }

  const start = (e: FormEvent) => {
    e.preventDefault()
    setFields({})
    setup.mutate(password, { onSuccess: setSecret, onError: (err) => handleError(err, setFields) })
  }
  const confirm = (e: FormEvent) => {
    e.preventDefault()
    setFields({})
    enable.mutate(code, {
      onSuccess: (r) => setCodes(r.recovery_codes),
      onError: (err) => {
        handleError(err, setFields)
        setCode("")
      },
    })
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent className="grid-cols-1 sm:max-w-lg" showCloseButton={!codes}>
        {codes ? (
          <RecoveryCodesView
            title="Save your recovery codes"
            description="Two-factor authentication is on, and your other sessions were signed out."
            codes={codes}
            onDone={() => {
              toast.success("Two-factor authentication enabled")
              onClose()
            }}
          />
        ) : secret ? (
          <form className="min-w-0 space-y-5" onSubmit={confirm} noValidate>
            <DialogHeader>
              <DialogTitle>Scan with your authenticator app</DialogTitle>
              <DialogDescription>Then enter the 6-digit code it shows to finish turning on two-factor authentication.</DialogDescription>
            </DialogHeader>
            <div className="flex flex-col items-center gap-4 sm:flex-row sm:items-start">
              {/* White quiet zone so the code scans in dark mode too. */}
              <div className="shrink-0 rounded-lg border bg-white p-3">
                <QRCodeSVG value={secret.otpauth_uri} size={168} marginSize={0} title="Authenticator setup QR code" />
              </div>
              <div className="min-w-0 flex-1 space-y-1.5 self-stretch">
                <div className="text-xs text-muted-foreground">Can&apos;t scan? Enter this key in the app instead:</div>
                <CopyField value={secret.secret.replace(/(.{4})(?=.)/g, "$1 ")} wrap />
                <p className="text-xs text-muted-foreground">Time-based, SHA-1, 6 digits, 30 seconds.</p>
              </div>
            </div>
            <Field data-invalid={!!fields.code}>
              <FieldLabel htmlFor="tfa-code">Authentication code</FieldLabel>
              <Input
                id="tfa-code"
                inputMode="numeric"
                autoComplete="one-time-code"
                placeholder="123456"
                maxLength={7}
                autoFocus
                className="font-mono tracking-[0.3em]"
                aria-invalid={!!fields.code}
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/[^0-9 ]/g, ""))}
              />
              {fields.code && <FieldError>{fields.code}</FieldError>}
            </Field>
            <DialogFooter>
              <Button type="submit" disabled={code.replace(/ /g, "").length !== 6 || enable.isPending}>
                {enable.isPending && <Spinner />} Turn on
              </Button>
            </DialogFooter>
          </form>
        ) : (
          <form className="space-y-5" onSubmit={start} noValidate>
            <DialogHeader>
              <DialogTitle>Set up two-factor authentication</DialogTitle>
              <DialogDescription>Confirm your password to continue.</DialogDescription>
            </DialogHeader>
            <PasswordField id="tfa-pw" value={password} onChange={setPassword} error={fields.password} />
            <DialogFooter>
              <Button type="submit" disabled={!password || setup.isPending}>
                {setup.isPending && <Spinner />} Continue
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

function DisableDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const disable = useDisableTwoFactor()
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [fields, setFields] = useState<Record<string, string>>({})
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <form
          className="space-y-5"
          noValidate
          onSubmit={(e) => {
            e.preventDefault()
            setFields({})
            disable.mutate(
              { password, code },
              {
                onSuccess: () => {
                  toast.success("Two-factor authentication turned off")
                  onClose()
                },
                onError: (err) => handleError(err, setFields),
              },
            )
          }}
        >
          <DialogHeader>
            <DialogTitle>Turn off two-factor authentication?</DialogTitle>
            <DialogDescription>Your account will be protected by your password alone. Your recovery codes stop working.</DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <PasswordField id="tfa-off-pw" value={password} onChange={setPassword} error={fields.password} />
            <CodeField id="tfa-off-code" value={code} onChange={setCode} error={fields.code} />
          </FieldGroup>
          <DialogFooter>
            <Button type="submit" variant="destructive" disabled={!password || !code.trim() || disable.isPending}>
              {disable.isPending && <Spinner />} Turn off
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function RegenerateDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const regenerate = useRegenerateRecoveryCodes()
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [fields, setFields] = useState<Record<string, string>>({})
  const [codes, setCodes] = useState<string[] | null>(null)
  return (
    <Dialog open={open} onOpenChange={(o) => !o && !codes && onClose()}>
      <DialogContent className="grid-cols-1 sm:max-w-lg" showCloseButton={!codes}>
        {codes ? (
          <RecoveryCodesView title="Your new recovery codes" description="Your previous recovery codes no longer work." codes={codes} onDone={onClose} />
        ) : (
          <form
            className="space-y-5"
            noValidate
            onSubmit={(e) => {
              e.preventDefault()
              setFields({})
              regenerate.mutate({ password, code }, { onSuccess: (r) => setCodes(r.recovery_codes), onError: (err) => handleError(err, setFields) })
            }}
          >
            <DialogHeader>
              <DialogTitle>Generate new recovery codes</DialogTitle>
              <DialogDescription>All of your existing recovery codes, used or not, will be replaced.</DialogDescription>
            </DialogHeader>
            <FieldGroup>
              <PasswordField id="tfa-rc-pw" value={password} onChange={setPassword} error={fields.password} />
              <CodeField id="tfa-rc-code" value={code} onChange={setCode} error={fields.code} />
            </FieldGroup>
            <DialogFooter>
              <Button type="submit" disabled={!password || !code.trim() || regenerate.isPending}>
                {regenerate.isPending && <Spinner />} Generate codes
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

function PasswordField({ id, value, onChange, error }: { id: string; value: string; onChange: (v: string) => void; error?: string }) {
  return (
    <Field data-invalid={!!error}>
      <FieldLabel htmlFor={id}>Password</FieldLabel>
      <Input
        id={id}
        type="password"
        autoComplete="current-password"
        autoFocus
        aria-invalid={!!error}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      {error && <FieldError>{error}</FieldError>}
    </Field>
  )
}

function CodeField({ id, value, onChange, error }: { id: string; value: string; onChange: (v: string) => void; error?: string }) {
  return (
    <Field data-invalid={!!error}>
      <FieldLabel htmlFor={id}>Authentication code or recovery code</FieldLabel>
      <Input
        id={id}
        autoComplete="one-time-code"
        autoCapitalize="none"
        spellCheck={false}
        className="font-mono"
        aria-invalid={!!error}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      {error && <FieldError>{error}</FieldError>}
    </Field>
  )
}

function RecoveryCodesView({ title, description, codes, onDone }: { title: string; description: string; codes: string[]; onDone: () => void }) {
  const [saved, setSaved] = useState(false)
  const text = codes.join("\n")
  const download = () => {
    const body = `DBVault recovery codes\nGenerated: ${new Date().toISOString()}\nEach code can be used once to sign in without your authenticator app.\n\n${text}\n`
    const url = URL.createObjectURL(new Blob([body], { type: "text/plain" }))
    const a = document.createElement("a")
    a.href = url
    a.download = "dbvault-recovery-codes.txt"
    a.click()
    URL.revokeObjectURL(url)
  }
  return (
    <div className="min-w-0 space-y-5">
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
      </DialogHeader>
      <Alert>
        <AlertTriangle />
        <AlertDescription>
          Each code signs you in once if you lose your authenticator. Store them somewhere safe, like a password manager. They won&apos;t be shown again.
        </AlertDescription>
      </Alert>
      <ul className="grid grid-cols-2 gap-x-6 gap-y-1.5 rounded-lg border bg-muted/40 p-4 font-mono text-sm" aria-label="Recovery codes">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" size="sm" onClick={download}>
          <Download /> Download
        </Button>
        <span className="inline-flex items-center gap-1 text-sm text-muted-foreground">
          <CopyButton value={text} label="Copy recovery codes" /> Copy all
        </span>
      </div>
      <label className="flex items-center gap-2 text-sm">
        <Checkbox checked={saved} onCheckedChange={(v) => setSaved(v === true)} />
        I&apos;ve saved my recovery codes
      </label>
      <DialogFooter>
        <Button disabled={!saved} onClick={onDone}>
          Done
        </Button>
      </DialogFooter>
    </div>
  )
}
