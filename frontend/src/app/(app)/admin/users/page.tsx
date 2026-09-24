import type { Metadata } from "next"

import { AdminUsers } from "@/components/admin/admin-users"

export const metadata: Metadata = { title: "Users · Instance admin" }

export default function AdminUsersPage() {
  return <AdminUsers />
}
