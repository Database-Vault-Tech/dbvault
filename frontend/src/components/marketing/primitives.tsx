import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

export function Section({ id, className, children, labelledBy }: { id?: string; className?: string; children: ReactNode; labelledBy?: string }) {
  return (
    <section id={id} aria-labelledby={labelledBy} className={cn("scroll-mt-20 border-t border-border/60 py-20 sm:py-28", className)}>
      <div className="mx-auto w-full max-w-6xl px-4 sm:px-6 lg:px-8">{children}</div>
    </section>
  )
}

export function SectionHeading({
  id,
  eyebrow,
  title,
  description,
  className,
}: {
  id: string
  eyebrow: string
  title: ReactNode
  description?: ReactNode
  className?: string
}) {
  return (
    <div className={cn("max-w-2xl space-y-3", className)}>
      <p className="font-mono text-xs font-medium tracking-wider text-brand uppercase">{eyebrow}</p>
      <h2 id={id} className="text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
        {title}
      </h2>
      {description && <p className="text-base text-pretty text-muted-foreground sm:text-lg">{description}</p>}
    </div>
  )
}

/** A terminal window frame for code snippets. */
export function Terminal({ title, children, className }: { title: string; children: ReactNode; className?: string }) {
  return (
    // Terminals stay dark in both themes, like a real terminal.
    <div className={cn("dark min-w-0 overflow-hidden rounded-xl border bg-card text-card-foreground shadow-2xl shadow-black/20 dark:shadow-black/30", className)}>
      <div className="flex items-center gap-1.5 border-b px-4 py-2.5">
        <span className="size-2.5 rounded-full bg-muted-foreground/25" />
        <span className="size-2.5 rounded-full bg-muted-foreground/25" />
        <span className="size-2.5 rounded-full bg-muted-foreground/25" />
        <span className="ml-2 truncate font-mono text-xs text-muted-foreground">{title}</span>
      </div>
      <pre className="overflow-x-auto p-4 font-mono text-[12.5px] leading-6 sm:text-[13px]">{children}</pre>
    </div>
  )
}

export function Prompt({ children }: { children: ReactNode }) {
  return (
    <span className="block">
      <span className="text-brand select-none">$ </span>
      {children}
    </span>
  )
}
