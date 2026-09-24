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
        if (!(await copyText(value))) return
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
      }}
    >
      {copied ? <Check className="text-success" /> : <Copy />}
    </Button>
  )
}

/**
 * navigator.clipboard only exists in secure contexts, so self-hosted installs served over
 * plain http (e.g. http://10.0.0.5:3000) fall back to a hidden textarea + execCommand.
 */
async function copyText(value: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(value)
      return true
    }
  } catch {
    // fall through to the legacy path
  }
  const ta = document.createElement("textarea")
  ta.value = value
  ta.setAttribute("readonly", "")
  ta.style.position = "fixed"
  ta.style.opacity = "0"
  document.body.appendChild(ta)
  ta.select()
  try {
    return document.execCommand("copy")
  } catch {
    return false
  } finally {
    ta.remove()
  }
}

/** A monospace value with a copy button (checksums, keys, tokens). `wrap` shows it in full instead of truncating. */
export function CopyField({ value, className, wrap = false }: { value: string; className?: string; wrap?: boolean }) {
  return (
    <div
      className={cn(
        "flex min-w-0 gap-1 rounded-md border bg-muted/40 py-0.5 pr-0.5 pl-2.5",
        wrap ? "items-start" : "items-center",
        className,
      )}
    >
      <code className={cn("min-w-0 flex-1 font-mono text-xs", wrap ? "py-1.5 leading-relaxed break-all whitespace-pre-wrap select-all" : "truncate")}>
        {value}
      </code>
      <CopyButton value={value} />
    </div>
  )
}
