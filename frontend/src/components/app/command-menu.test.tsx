import { screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import { mockFetch, renderWithProviders } from "@/test/utils"

import { CommandMenu } from "./command-menu"

const push = vi.fn()
vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }))

const database = { id: "db-1", name: "Maestro", host: "db.internal", database: "maestro" }

describe("CommandMenu", () => {
  // cmdk's children need the <Command> context its dialog provides. Without
  // it, opening the palette threw and took the whole page down.
  it("opens the palette and lists databases", async () => {
    mockFetch({ "GET /api/databases": () => ({ body: { data: [database] } }) })
    renderWithProviders(<CommandMenu />)

    await userEvent.click(screen.getByRole("button", { name: /search/i }))

    expect(await screen.findByPlaceholderText(/Search pages/i)).toBeInTheDocument()
    expect(await screen.findByText("Maestro")).toBeInTheDocument()
    expect(screen.getByText("Add database")).toBeInTheDocument()
  })

  it("navigates to the selected database", async () => {
    mockFetch({ "GET /api/databases": () => ({ body: { data: [database] } }) })
    renderWithProviders(<CommandMenu />)

    await userEvent.click(screen.getByRole("button", { name: /search/i }))
    await userEvent.click(await screen.findByText("Maestro"))

    await waitFor(() => expect(push).toHaveBeenCalledWith("/databases/db-1"))
  })
})
