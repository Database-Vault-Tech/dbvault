import type { Metadata } from "next"
import { Suspense } from "react"

import { MaskingEditor } from "@/components/databases/masking-editor"

export const metadata: Metadata = { title: "Data masking" }

export default async function MaskingPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params
  return (
    <Suspense>
      <MaskingEditor id={id} />
    </Suspense>
  )
}
