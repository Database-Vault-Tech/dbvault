"use client"

import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { useState } from "react"
import { toast } from "sonner"

import { AuthHeader } from "@/components/auth/auth-form"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { api, errorMessage } from "@/lib/api"

export function ResetPasswordForm() {
  const token = useSearchParams().get("token") ?? ""
  const router = useRouter()
  const [password, setPassword] = useState("")
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (!token) {
    return (
      <Alert variant="destructive">
        <AlertDescription>
          This reset link is incomplete. <Link href="/forgot-password">Request a new one.</Link>
        </AlertDescription>
      </Alert>
    )
  }
  return (
    <>
      <AuthHeader title="Choose a new password" description="All other sessions will be signed out." />
      <form
        onSubmit={async (e) => {
          e.preventDefault()
          setPending(true)
          setError(null)
          try {
            await api.post("/auth/password/reset", { token, password }, { noOrg: true })
            toast.success("Password updated. Sign in with your new password.")
            router.replace("/login")
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
            <FieldLabel htmlFor="password">New password</FieldLabel>
            <Input id="password" type="password" autoComplete="new-password" minLength={10} required value={password} onChange={(e) => setPassword(e.target.value)} />
            <FieldDescription>At least 10 characters.</FieldDescription>
          </Field>
          <Button type="submit" size="lg" disabled={pending || password.length < 10}>
            {pending && <Spinner />} Update password
          </Button>
        </FieldGroup>
      </form>
    </>
  )
}
