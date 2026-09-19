import { describe, expect, it } from "vitest"

import { formatBytes, formatDuration, formatPercent, formatRelative, humanizeAction, storageShort } from "./format"

describe("formatBytes", () => {
  it.each([
    [null, "—"],
    [0, "0 B"],
    [1023, "1023 B"],
    [1024, "1 KB"],
    [505413632, "482 MB"],
    [197568495616, "184 GB"],
    [1536, "1.5 KB"],
  ])("%s → %s", (input, expected) => {
    expect(formatBytes(input as number | null)).toBe(expected)
  })
})

describe("formatDuration", () => {
  it.each([
    [null, "—"],
    [450, "450ms"],
    [41_000, "41s"],
    [134_000, "2m 14s"],
    [63_000, "1m 03s"],
    [3_780_000, "1h 03m"],
  ])("%s → %s", (input, expected) => {
    expect(formatDuration(input as number | null)).toBe(expected)
  })
})

describe("formatRelative", () => {
  const now = new Date("2026-09-19T12:00:00Z")
  it("formats past and future times", () => {
    expect(formatRelative("2026-09-19T11:48:00Z", now)).toBe("12 minutes ago")
    expect(formatRelative("2026-09-19T11:00:00Z", now)).toBe("1 hour ago")
    expect(formatRelative("2026-09-19T15:00:00Z", now)).toBe("in 3 hours")
    expect(formatRelative("2026-09-19T11:59:55Z", now)).toBe("just now")
    expect(formatRelative(null, now)).toBe("—")
  })
})

describe("misc", () => {
  it("formats percentages, actions and storage labels", () => {
    expect(formatPercent(1)).toBe("100%")
    expect(formatPercent(0.9567)).toBe("95.7%")
    expect(humanizeAction("backup.verification_requested")).toBe("Backup verification requested")
    expect(storageShort("r2")).toBe("R2")
  })
})
