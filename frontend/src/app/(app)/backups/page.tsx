import type { Metadata } from "next"

import { BackupsView } from "@/components/backups/backups-view"

export const metadata: Metadata = { title: "Backups" }

export default function BackupsPage() {
  return <BackupsView />
}
