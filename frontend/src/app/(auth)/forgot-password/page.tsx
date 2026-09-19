"use client"

import Link from "next/link"
import { useState } from "react"

import { AuthHeader } from "@/components/auth/auth-form"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { api, errorMessage } from "@/lib/api"

export default function ForgotPasswordPage() {
  const [email, setEmail] = useState("")
  const [pending, setPending] = useState(false)
  const [result, setResult] = useState<{ message: string; email_enabled: boolean } | null>(null)
  const [error, setError] = useState<string | null>(null)

  return (
    <>
      <AuthHeader title="Reset your password" description="Enter your account email and we'll send you a reset link." />
      {result ? (
        <Alert>
          <AlertTitle>Check your inbox</AlertTitle>
          <AlertDescription>
            <p>{result.message}</p>
            {!result.email_enabled && (
              <p>
                Email isn&apos;t configured on this DBVault server (SMTP_HOST), so no message can be sent. Ask your administrator to reset
                your password.
              </p>
            )}
          </AlertDescription>
        </Alert>
      ) : (
        <form
          onSubmit={async (e) => {
            e.preventDefault()
            setPending(true)
            setError(null)
            try {
              setResult(await api.post("/auth/password/forgot", { email }, { noOrg: true }))
            } catch (err) {
              setError(errorMessage(err))
            } finally {
              setPending(false)
            }
          }}
        >
          <FieldGroup>
            {error && (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
            <Field>
              <FieldLabel htmlFor="email">Email</FieldLabel>
              <Input id="email" type="email" required autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />
            </Field>
            <Button type="submit" size="lg" disabled={pending || !email}>
              {pending && <Spinner />} Send reset link
            </Button>
          </FieldGroup>
        </form>
      )}
      <p className="mt-6 text-sm text-muted-foreground">
        <Link href="/login" className="hover:text-foreground">
          ← Back to sign in
        </Link>
      </p>
    </>
  )
}
