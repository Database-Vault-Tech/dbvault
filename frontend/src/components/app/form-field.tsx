"use client"

import type { ReactNode } from "react"
import type { FieldError as RHFFieldError } from "react-hook-form"

import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"

/** Label + control + description + error, wired for react-hook-form. */
export function FormField({
  id,
  label,
  description,
  error,
  children,
  className,
}: {
  id: string
  label: ReactNode
  description?: ReactNode
  error?: RHFFieldError | { message?: string }
  children: ReactNode
  className?: string
}) {
  return (
    <Field data-invalid={!!error} className={className}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      {children}
      {description && !error && <FieldDescription>{description}</FieldDescription>}
      <FieldError errors={error ? [error] : undefined} />
    </Field>
  )
}
