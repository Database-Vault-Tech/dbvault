"use client"

import { AlertTriangle, RotateCw } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { ApiError, errorMessage } from "@/lib/api"

/** Friendly error display; never shows stack traces. */
export function ErrorState({ error, retry, title = "Couldn't load this page" }: { error: unknown; retry?: () => void; title?: string }) {
  const requestId = error instanceof ApiError ? error.requestId : undefined
  return (
    <Alert variant="destructive" className="items-start">
      <AlertTriangle />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>
        <p>{errorMessage(error)}</p>
        {requestId && <p className="font-mono text-xs opacity-70">Request ID: {requestId}</p>}
        {retry && (
          <Button variant="outline" size="sm" className="mt-2" onClick={retry}>
            <RotateCw /> Try again
          </Button>
        )}
      </AlertDescription>
    </Alert>
  )
}
