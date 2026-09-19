import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { StatusBadge, toneFor } from "./status"

describe("status mapping", () => {
  it.each([
    ["completed", "success"],
    ["passed", "success"],
    ["running", "running"],
    ["verifying", "running"],
    ["queued", "queued"],
    ["failed", "error"],
    ["unavailable", "warning"],
    ["cancelled", "neutral"],
    ["something-new", "neutral"],
  ])("%s → %s", (status, tone) => {
    expect(toneFor(status)).toBe(tone)
  })

  it("never labels an unavailable verification as passed", () => {
    render(<StatusBadge status="unavailable" />)
    expect(screen.getByText("Unavailable")).toBeInTheDocument()
    expect(screen.queryByText(/verified|pass/i)).toBeNull()
  })
})
