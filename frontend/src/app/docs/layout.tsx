import type { Metadata } from "next"

import { DocsHeader } from "@/components/docs/docs-header"
import { DocsSidebar } from "@/components/docs/docs-nav"

export const metadata: Metadata = {
  title: { template: "%s · DBVault docs", default: "Documentation" },
}

export default function DocsLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <DocsHeader />
      <div className="mx-auto flex w-full max-w-7xl gap-10 px-4 sm:px-6 lg:px-8">
        <DocsSidebar />
        <main className="min-w-0 flex-1 py-10">{children}</main>
      </div>
    </div>
  )
}
