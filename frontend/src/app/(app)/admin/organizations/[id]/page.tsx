import type { Metadata } from "next"

import { AdminOrganization } from "@/components/admin/admin-organization"

export const metadata: Metadata = { title: "Organization · Instance admin" }

export default async function AdminOrganizationPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params
  return <AdminOrganization id={id} />
}
