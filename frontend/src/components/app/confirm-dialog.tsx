"use client"

import { AlertTriangle } from "lucide-react"
import { useState, type ReactNode } from "react"

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Spinner } from "@/components/ui/spinner"
import { cn } from "@/lib/utils"

/**
 * Confirmation for destructive actions. When `phrase` is set the user must
 * type it exactly (e.g. "RESTORE") before the action is enabled.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = "Confirm",
  phrase,
  destructive = true,
  pending,
  onConfirm,
  children,
  icon,
  className,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: ReactNode
  confirmLabel?: string
  phrase?: string
  destructive?: boolean
  pending?: boolean
  onConfirm: () => void
  children?: ReactNode
  /** Replaces the default warning icon in the title. */
  icon?: ReactNode
  /** Extra classes for the dialog panel (e.g. a wider max width). */
  className?: string
}) {
  const [typed, setTyped] = useState("")
  const ready = !phrase || typed === phrase
  return (
    <AlertDialog
      open={open}
      onOpenChange={(o) => {
        if (!o) setTyped("")
        onOpenChange(o)
      }}
    >
      <AlertDialogContent className={cn("min-w-0 data-[size=default]:sm:max-w-md", className)}>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            {icon ?? (destructive && <AlertTriangle className="size-5 shrink-0 text-destructive" />)}
            {title}
          </AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="min-w-0 space-y-2 text-sm break-words text-muted-foreground">{description}</div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        {children}
        {phrase && (
          <div className="space-y-2">
            <Label htmlFor="confirm-phrase">
              Type <span className="font-mono font-semibold text-foreground">{phrase}</span> to confirm
            </Label>
            <Input
              id="confirm-phrase"
              autoComplete="off"
              autoFocus
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              aria-invalid={typed.length > 0 && !ready}
            />
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>Cancel</AlertDialogCancel>
          <Button variant={destructive ? "destructive" : "default"} disabled={!ready || pending} onClick={onConfirm}>
            {pending && <Spinner />}
            {confirmLabel}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
