import type { Metadata } from "next"

import { DatabasesView } from "@/components/databases/databases-view"

export const metadata: Metadata = { title: "Databases" }

export default function DatabasesPage() {
  return <DatabasesView />
}
