import type { Metadata } from "next"
import { Suspense } from "react"

import { SchedulesView } from "@/components/schedules/schedules-view"

export const metadata: Metadata = { title: "Schedules" }

export default function SchedulesPage() {
  return (
    <Suspense>
      <SchedulesView />
    </Suspense>
  )
}
