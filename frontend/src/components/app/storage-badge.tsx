import { Cloud, Database, HardDrive, Server } from "lucide-react"

import { storageShort } from "@/lib/format"
import type { StorageType } from "@/lib/types"
import { cn } from "@/lib/utils"

const ICONS = { local: HardDrive, s3: Cloud, r2: Cloud, minio: Server } as const

export function StorageBadge({ type, name, className }: { type: StorageType; name?: string; className?: string }) {
  const Icon = ICONS[type] ?? Database
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-sm", className)}>
      <Icon className="size-3.5 text-muted-foreground" />
      <span>{name ?? storageShort(type)}</span>
    </span>
  )
}
