"use client"

import { zodResolver } from "@hookform/resolvers/zod"
import { useQueryClient } from "@tanstack/react-query"
import { Mail } from "lucide-react"
import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { useState } from "react"
import { useForm } from "react-hook-form"

import { AuthHeader, safeNext } from "@/components/auth/auth-form"
import { AuthInput, AuthPasswordInput } from "@/components/auth/auth-input"
import { FormField } from "@/components/app/form-field"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { FieldGroup } from "@/components/ui/field"
import { Spinner } from "@/components/ui/spinner"
import { api, errorMessage } from "@/lib/api"
import { loginSchema, type LoginValues } from "@/lib/schemas"

export function LoginForm() {
  const router = useRouter()
  const params = useSearchParams()
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const form = useForm<LoginValues>({ resolver: zodResolver(loginSchema), defaultValues: { email: "", password: "" } })

  const onSubmit = form.handleSubmit(async (values) => {
    setError(null)
    try {
      await api.post("/auth/login", values, { noOrg: true })
      qc.clear()
      router.replace(safeNext(params.get("next")))
    } catch (err) {
      setError(errorMessage(err))
    }
  })

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
