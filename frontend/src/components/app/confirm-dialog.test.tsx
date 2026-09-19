import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import { ConfirmDialog } from "./confirm-dialog"

describe("ConfirmDialog", () => {
  it("requires the exact phrase before a destructive restore", async () => {
    const onConfirm = vi.fn()
    render(
      <ConfirmDialog
        open
        onOpenChange={() => {}}
        title="Restore over production?"
        description="Restoring this backup may overwrite existing data."
        confirmLabel="Restore"
        phrase="RESTORE"
        onConfirm={onConfirm}
      />,
    )
    const button = screen.getByRole("button", { name: "Restore" })
    expect(button).toBeDisabled()

    const input = screen.getByLabelText(/Type/)
    await userEvent.type(input, "restore")
    expect(button).toBeDisabled()

    await userEvent.clear(input)
    await userEvent.type(input, "RESTORE")
    expect(button).toBeEnabled()
    await userEvent.click(button)
    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it("confirms immediately when no phrase is required", async () => {
    const onConfirm = vi.fn()
    render(<ConfirmDialog open onOpenChange={() => {}} title="Delete?" description="x" confirmLabel="Delete" onConfirm={onConfirm} />)
    await userEvent.click(screen.getByRole("button", { name: "Delete" }))
    expect(onConfirm).toHaveBeenCalledOnce()
  })
})
