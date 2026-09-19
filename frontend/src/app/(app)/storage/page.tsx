import type { Metadata } from "next"
import { Suspense } from "react"

import { StorageView } from "@/components/storage/storage-view"

export const metadata: Metadata = { title: "Storage" }

export default function StoragePage() {
  return (
    <Suspense>
      <StorageView />
    </Suspense>
  )
}
