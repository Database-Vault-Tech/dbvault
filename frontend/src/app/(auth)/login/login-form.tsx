"use client"

import { zodResolver } from "@hookform/resolvers/zod"
import { useQueryClient } from "@tanstack/react-query"
import { KeyRound, Mail, ShieldCheck } from "lucide-react"
import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { type FormEvent, useState } from "react"
import { useForm } from "react-hook-form"

import { AuthHeader, safeNext } from "@/components/auth/auth-form"
import { AuthInput, AuthPasswordInput } from "@/components/auth/auth-input"
import { FormField } from "@/components/app/form-field"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { FieldGroup } from "@/components/ui/field"
import { Spinner } from "@/components/ui/spinner"
import { ApiError, api, errorMessage } from "@/lib/api"
import { loginSchema, type LoginValues } from "@/lib/schemas"
import type { LoginResponse } from "@/lib/types"

export function LoginForm() {
  const router = useRouter()
  const params = useSearchParams()
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const [mfaToken, setMfaToken] = useState<string | null>(null)
  const form = useForm<LoginValues>({ resolver: zodResolver(loginSchema), defaultValues: { email: "", password: "" } })

  const signedIn = () => {
    qc.clear()
    router.replace(safeNext(params.get("next")))
  }

  const onSubmit = form.handleSubmit(async (values) => {
    setError(null)
    try {
      const res = await api.post<LoginResponse>("/auth/login", values, { noOrg: true })
      if (res.mfa_required) setMfaToken(res.mfa_token)
      else signedIn()
    } catch (err) {
      setError(errorMessage(err))
    }
  })

  if (mfaToken) {
    return (
      <TwoFactorStep
        mfaToken={mfaToken}
        onSuccess={signedIn}
        onRestart={(message) => {
          setMfaToken(null)
          setError(message ?? null)
          form.resetField("password")
        }}
      />
    )
  }

  return (
    <>
      <AuthHeader
        title="Welcome back"
        description={
          <>
            New to DBVault?{" "}
            <Link href="/register" className="font-medium text-foreground underline-offset-4 hover:underline">
              Create an account
            </Link>
          </>
        }
      />
      <form onSubmit={onSubmit} noValidate>
        <FieldGroup>
          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          <FormField id="email" label="Email" error={form.formState.errors.email}>
            <AuthInput id="email" type="email" icon={Mail} placeholder="you@company.com" autoComplete="email" autoFocus {...form.register("email")} />
          </FormField>
          <FormField
            id="password"
            label={
              <span className="flex w-full items-center justify-between">
                Password
                <Link href="/forgot-password" className="text-xs font-normal text-muted-foreground hover:text-foreground">
                  Forgot password?
                </Link>
              </span>
            }
            error={form.formState.errors.password}
          >
            <AuthPasswordInput id="password" placeholder="Enter your password" autoComplete="current-password" {...form.register("password")} />
          </FormField>
          <Button type="submit" size="lg" className="h-11 text-[15px]" disabled={form.formState.isSubmitting}>
            {form.formState.isSubmitting && <Spinner />} Sign in
          </Button>
        </FieldGroup>
      </form>
    </>
  )
}

/** Second sign-in step for accounts with two-factor authentication. */
function TwoFactorStep({ mfaToken, onSuccess, onRestart }: { mfaToken: string; onSuccess: () => void; onRestart: (message?: string) => void }) {
  const [recovery, setRecovery] = useState(false)
  const [code, setCode] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)
    setPending(true)
    try {
      await api.post("/auth/login/2fa", { mfa_token: mfaToken, code }, { noOrg: true })
      onSuccess()
    } catch (err) {
      setPending(false)
      if (err instanceof ApiError && err.code === "mfa_expired") return onRestart(err.message)
      setError(err instanceof ApiError ? (err.fields.code ?? err.message) : errorMessage(err))
      setCode("")
    }
  }

  return (
    <>
      <AuthHeader
        title="Two-factor authentication"
        description={recovery ? "Enter one of the recovery codes you saved when you turned on two-factor authentication." : "Enter the 6-digit code from your authenticator app."}
      />
      <form onSubmit={submit} noValidate>
        <FieldGroup>
          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          <FormField id="code" label={recovery ? "Recovery code" : "Authentication code"}>
            {recovery ? (
              <AuthInput
                key="recovery"
                id="code"
                icon={KeyRound}
                placeholder="xxxxx-xxxxx"
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                autoFocus
                className="font-mono"
                value={code}
                onChange={(e) => setCode(e.target.value)}
              />
            ) : (
              <AuthInput
                key="totp"
                id="code"
                icon={ShieldCheck}
                placeholder="123456"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={7}
                autoFocus
                className="font-mono tracking-[0.3em]"
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/[^0-9 ]/g, ""))}
              />
            )}
          </FormField>
          <Button type="submit" size="lg" className="h-11 text-[15px]" disabled={pending || !code.trim()}>
            {pending && <Spinner />} Verify
          </Button>
          <div className="flex items-center justify-between text-sm">
            <button
              type="button"
              className="text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
              onClick={() => {
                setRecovery((r) => !r)
                setCode("")
                setError(null)
              }}
            >
              {recovery ? "Use your authenticator app" : "Use a recovery code"}
            </button>
            <button type="button" className="text-muted-foreground underline-offset-4 hover:text-foreground hover:underline" onClick={() => onRestart()}>
              Back
            </button>
          </div>
        </FieldGroup>
      </form>
    </>
  )
}
