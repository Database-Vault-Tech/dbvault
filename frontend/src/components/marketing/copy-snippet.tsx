"use client"

import { Check, Copy } from "lucide-react"
import { useState } from "react"

import { cn } from "@/lib/utils"

/** Inline command chip with a copy-to-clipboard button. */
export function CopySnippet({ command, className }: { command: string; className?: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className={cn("inline-flex max-w-full items-center gap-3 rounded-lg border bg-card/80 py-1.5 pr-1.5 pl-3.5 font-mono text-sm", className)}>
      <span className="text-brand select-none">$</span>
      <code className="truncate">{command}</code>
      <button
        type="button"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(command)
            setCopied(true)
            setTimeout(() => setCopied(false), 1500)
          } catch {
            // Clipboard unavailable (insecure context); nothing to do.
          }
        }}
        aria-label={copied ? "Copied" : `Copy command: ${command}`}
        className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        {copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
      </button>
    </div>
  )
}
