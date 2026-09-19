import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import { AuthPasswordInput } from "./auth-input"

describe("AuthPasswordInput", () => {
  it("toggles password visibility with the eye button", async () => {
    render(
      <>
        <label htmlFor="password">Password</label>
        <AuthPasswordInput id="password" defaultValue="s3cret-value" />
      </>,
    )
    const input = screen.getByLabelText("Password")
    expect(input).toHaveAttribute("type", "password")

    await userEvent.click(screen.getByRole("button", { name: "Show password" }))
    expect(input).toHaveAttribute("type", "text")
    expect(screen.getByRole("button", { name: "Hide password" })).toHaveAttribute("aria-pressed", "true")

    await userEvent.click(screen.getByRole("button", { name: "Hide password" }))
    expect(input).toHaveAttribute("type", "password")
  })
})
