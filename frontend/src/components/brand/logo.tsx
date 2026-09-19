import { cn } from "@/lib/utils"

/** The DBVault mark: a vault outline with a solid core. */
export function LogoMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" aria-hidden="true" className={cn("size-6", className)}>
      <rect x="2" y="2" width="28" height="28" rx="7.5" fill="none" strokeWidth="2.5" className="stroke-brand" />
      <rect x="11.5" y="11.5" width="9" height="9" rx="2" className="fill-brand" />
    </svg>
  )
}

export function Logo({ className, markClassName }: { className?: string; markClassName?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-2 font-semibold tracking-tight", className)}>
      <LogoMark className={markClassName} />
      <span>DBVault</span>
    </span>
  )
}
