import type { Metadata } from "next"

// Private, sign-in-only pages: keep them out of search results.
export const metadata: Metadata = { robots: { index: false, follow: false } }

import { AppShell } from "@/components/app/app-shell"

export default function AppLayout({ children }: { children: React.ReactNode }) {
  return <AppShell>{children}</AppShell>
}
