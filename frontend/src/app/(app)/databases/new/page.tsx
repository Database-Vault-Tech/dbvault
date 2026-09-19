import type { Metadata } from "next"

import { NewDatabaseView } from "@/components/databases/new-database-view"

export const metadata: Metadata = { title: "Add database" }

export default function NewDatabasePage() {
  return <NewDatabaseView />
}
