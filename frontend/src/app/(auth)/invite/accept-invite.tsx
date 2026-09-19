"use client"

import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { toast } from "sonner"

import { AuthHeader } from "@/components/auth/auth-form"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { ORG_STORAGE_KEY, errorMessage } from "@/lib/api"
import { useAcceptInvitation, useMe } from "@/lib/queries"

export function AcceptInvite() {
  const token = useSearchParams().get("token") ?? ""
  const router = useRouter()
  const me = useMe()
  const accept = useAcceptInvitation()
  const next = `/invite?token=${encodeURIComponent(token)}`

  if (me.isPending) return <Spinner className="mx-auto" />
  if (!me.data) {
    return (
      <>
        <AuthHeader title="You've been invited" description="Sign in or create an account with the invited email address to join the organization." />
        <div className="flex flex-col gap-2">
          <Button asChild size="lg">
            <Link href={`/login?next=${encodeURIComponent(next)}`}>Sign in to accept</Link>
          </Button>
          <Button asChild size="lg" variant="outline">
            <Link href={`/register`}>Create an account</Link>
          </Button>
          <p className="pt-2 text-xs text-muted-foreground">After creating an account, open the invitation link again to accept it.</p>
        </div>
      </>
    )
  }
  return (
    <>
      <AuthHeader title="Join organization" description={<>Signed in as {me.data.user.email}.</>} />
      {accept.error && (
        <Alert variant="destructive" className="mb-4">
          <AlertDescription>{errorMessage(accept.error)}</AlertDescription>
        </Alert>
      )}
      <Button
        size="lg"
        className="w-full"
        disabled={!token || accept.isPending}
        onClick={() =>
          accept.mutate(token, {
            onSuccess: (org) => {
              try {
                window.localStorage.setItem(ORG_STORAGE_KEY, org.id)
              } catch {}
              toast.success(`You joined ${org.name} as ${org.role}`)
              router.replace("/dashboard")
            },
          })
        }
      >
        {accept.isPending && <Spinner />} Accept invitation
      </Button>
    </>
  )
}
