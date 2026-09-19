import type { Metadata } from "next"
import { Suspense } from "react"

import { RestoreView } from "@/components/restore/restore-view"

export const metadata: Metadata = { title: "Restore" }

export default function RestorePage() {
  return (
    <Suspense>
      <RestoreView />
    </Suspense>
  )
}
