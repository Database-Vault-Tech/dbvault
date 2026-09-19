import { Suspense } from "react"

import { AcceptInvite } from "./accept-invite"

export default function InvitePage() {
  return (
    <Suspense>
      <AcceptInvite />
    </Suspense>
  )
}
