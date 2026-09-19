import type { Metadata } from "next"
import { Suspense } from "react"

import { DatabaseDetail } from "@/components/databases/database-detail"

export const metadata: Metadata = { title: "Database" }

export default async function DatabasePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params
  return (
    <Suspense>
      <DatabaseDetail id={id} />
    </Suspense>
  )
}
