"use client"

import { Eye, EyeOff, Lock, type LucideIcon } from "lucide-react"
import { useState, type ComponentProps } from "react"

import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"

/** Taller, roomier inputs for the sign-in pages, with an optional leading icon. */
export function AuthInput({ icon: Icon, className, ...props }: ComponentProps<typeof Input> & { icon?: LucideIcon }) {
  return (
    <div className="group relative">
      {Icon && (
        <Icon
          aria-hidden
          className="pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2 text-muted-foreground transition-colors group-focus-within:text-brand"
        />
      )}
      <Input
        className={cn(
          "h-11 rounded-lg bg-muted/40 px-3.5 text-[15px] shadow-xs transition-[color,box-shadow,border-color] hover:border-foreground/25 focus-visible:border-brand focus-visible:ring-brand/20 md:text-[15px] dark:bg-input/25",
          Icon && "pl-10",
          className,
        )}
        {...props}
      />
    </div>
  )
}

/** Password field with a show/hide toggle. */
export function AuthPasswordInput({ className, ...props }: Omit<ComponentProps<typeof Input>, "type">) {
  const [visible, setVisible] = useState(false)
  return (
    <div className="relative">
      <AuthInput icon={Lock} type={visible ? "text" : "password"} className={cn("pr-11", className)} {...props} />
      <button
        type="button"
        onClick={() => setVisible((v) => !v)}
        aria-label={visible ? "Hide password" : "Show password"}
        aria-pressed={visible}
        aria-controls={props.id}
        className="absolute top-1/2 right-1.5 flex size-8 -translate-y-1/2 items-center justify-center rounded-md text-muted-foreground transition-colors outline-none hover:bg-muted hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        {visible ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
      </button>
    </div>
  )
}
