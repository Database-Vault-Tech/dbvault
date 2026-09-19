"use client"

import { Check, Copy } from "lucide-react"
import { useState } from "react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

export function CopyButton({ value, className, label = "Copy" }: { value: string; className?: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      aria-label={copied ? "Copied" : label}
      className={cn("text-muted-foreground", className)}
      onClick={async () => {
        await navigator.clipboard.writeText(value)
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
      }}
    >
      {copied ? <Check className="text-success" /> : <Copy />}
    </Button>
  )
}

/** A monospace value with a copy button (checksums, keys, tokens). */
export function CopyField({ value, className }: { value: string; className?: string }) {
  return (
    <div className={cn("flex min-w-0 items-center gap-1 rounded-md border bg-muted/40 py-0.5 pr-0.5 pl-2.5", className)}>
      <code className="min-w-0 flex-1 truncate font-mono text-xs">{value}</code>
      <CopyButton value={value} />
    </div>
  )
}
