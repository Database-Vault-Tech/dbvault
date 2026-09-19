"use client"

import { zodResolver } from "@hookform/resolvers/zod"
import { useQueryClient } from "@tanstack/react-query"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { useState } from "react"
import { useForm } from "react-hook-form"

import { AuthHeader } from "@/components/auth/auth-form"
import { FormField } from "@/components/app/form-field"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { FieldGroup } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { api, ApiError, errorMessage } from "@/lib/api"
import { registerSchema, type RegisterValues } from "@/lib/schemas"

export function RegisterForm() {
  const router = useRouter()
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const form = useForm<RegisterValues>({ resolver: zodResolver(registerSchema), defaultValues: { name: "", email: "", password: "" } })

  const onSubmit = form.handleSubmit(async (values) => {
    setError(null)
    try {
      await api.post("/auth/register", values, { noOrg: true })
      qc.clear()
      router.replace("/dashboard")
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length) {
        for (const [field, message] of Object.entries(err.fields)) {
          form.setError(field as keyof RegisterValues, { message })
        }
      } else {
        setError(errorMessage(err))
      }
    }
  })

  return (
    <>
      <AuthHeader
        title="Create your account"
        description={
          <>
            Already have an account?{" "}
            <Link href="/login" className="font-medium text-foreground underline-offset-4 hover:underline">
              Sign in
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
          <FormField id="name" label="Name" error={form.formState.errors.name}>
            <Input id="name" autoComplete="name" autoFocus {...form.register("name")} />
          </FormField>
          <FormField id="email" label="Work email" error={form.formState.errors.email}>
            <Input id="email" type="email" autoComplete="email" {...form.register("email")} />
          </FormField>
          <FormField id="password" label="Password" description="At least 10 characters." error={form.formState.errors.password}>
            <Input id="password" type="password" autoComplete="new-password" {...form.register("password")} />
          </FormField>
          <Button type="submit" size="lg" disabled={form.formState.isSubmitting}>
            {form.formState.isSubmitting && <Spinner />} Create account
          </Button>
          <p className="text-xs text-muted-foreground">
            We&apos;ll create a personal organization for you with its own backup encryption key.
          </p>
        </FieldGroup>
      </form>
    </>
  )
}
