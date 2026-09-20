import type { Metadata } from "next"
import { Suspense } from "react"

import { RegisterForm } from "./register-form"

export const metadata: Metadata = { title: "Create account" }

// RegisterForm reads the ?invite= token with useSearchParams, which a
// statically rendered page may only do inside a Suspense boundary.
export default function RegisterPage() {
  return (
    <Suspense>
      <RegisterForm />
    </Suspense>
  )
}
