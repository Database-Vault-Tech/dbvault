import type { RetentionPolicy, Schedule, ScheduleInput } from "@/lib/types"

export function retentionSummary(r: RetentionPolicy): string {
  if (!r.daily && !r.weekly && !r.monthly) return "Keep all"
  return [r.daily && `${r.daily} daily`, r.weekly && `${r.weekly} weekly`, r.monthly && `${r.monthly} monthly`].filter(Boolean).join(" · ")
}

export function scheduleToInput(s: Schedule, patch: Partial<ScheduleInput> = {}): ScheduleInput {
  return {
    database_id: s.database_id,
    storage_destination_id: s.storage_destination_id,
    name: s.name,
    preset: s.preset,
    cron_expression: s.cron_expression,
    timezone: s.timezone,
    enabled: s.enabled,
    compression: s.compression,
    encryption: s.encryption,
    retention: s.retention,
    verify_after_backup: s.verify_after_backup,
    ...patch,
  }
}

export function formatInZone(date: string, timeZone: string): string {
  try {
    return new Intl.DateTimeFormat("en", { timeZone, weekday: "short", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", hour12: false }).format(
      new Date(date),
    )
  } catch {
    return new Date(date).toLocaleString()
  }
}

export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"
  } catch {
    return "UTC"
  }
}

export function allTimeZones(): string[] {
  try {
    const zones = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.("timeZone") ?? []
    return zones.includes("UTC") ? zones : ["UTC", ...zones]
  } catch {
    return ["UTC"]
  }
}
