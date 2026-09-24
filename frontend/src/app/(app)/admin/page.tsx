import type { Metadata } from "next"

import { AdminOrganizations } from "@/components/admin/admin-organizations"

export const metadata: Metadata = { title: "Instance admin" }

export default function AdminPage() {
  return <AdminOrganizations />
}
