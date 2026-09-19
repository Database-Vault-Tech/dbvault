import type { Metadata } from "next"

import { BackupDetailView } from "@/components/backups/backup-detail"

export const metadata: Metadata = { title: "Backup" }

export default async function BackupPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params
  return <BackupDetailView id={id} />
}
