import { fireEvent, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import { mockFetch, renderWithProviders } from "@/test/utils"

import { DatabaseForm } from "./database-form"

const server = {
  version: "17.2",
  version_num: 170002,
  major: 17,
  size_bytes: 505413632,
  table_count: 42,
  full_version: "PostgreSQL 17.2",
  current_user: "app",
  is_superuser: false,
  in_recovery: false,
  latency_ms: 12,
}

describe("DatabaseForm", () => {
  it("fills fields from a pasted connection string and shows a successful test", async () => {
    const calls = mockFetch({
      "POST /api/databases/test": () => ({ body: { data: { ok: true, message: "Connection successful", server, tested_at: new Date().toISOString() } } }),
    })
    renderWithProviders(<DatabaseForm mode="create" submitLabel="Save database" onSubmit={vi.fn()} />)

    fireEvent.change(screen.getByLabelText(/connection string/i), {
      target: { value: "postgres://app:pa%24%24word@db.internal:6543/shop?sslmode=require" },
    })
    expect(screen.getByLabelText("Host")).toHaveValue("db.internal")
    expect(screen.getByLabelText("Port")).toHaveValue(6543)
    expect(screen.getByLabelText("Database")).toHaveValue("shop")
    expect(screen.getByLabelText("Username")).toHaveValue("app")
    expect(screen.getByLabelText("Name")).toHaveValue("shop")

    await userEvent.click(screen.getByRole("button", { name: /test connection/i }))
    expect(await screen.findByText("Connection successful")).toBeInTheDocument()
    expect(screen.getByText("PostgreSQL 17.2")).toBeInTheDocument()
    expect(calls[0].body).toMatchObject({ host: "db.internal", port: 6543, database: "shop", username: "app", password: "pa$$word", ssl_mode: "require" })
  })

  it("shows the reason when the connection fails", async () => {
    mockFetch({
      "POST /api/databases/test": () => ({ body: { data: { ok: false, message: "connection timeout: the server did not respond", tested_at: new Date().toISOString() } } }),
    })
    renderWithProviders(
      <DatabaseForm
        mode="create"
        submitLabel="Save database"
        onSubmit={vi.fn()}
        defaultValues={{ name: "prod", host: "10.0.0.9", database: "app", username: "app", password: "x" }}
      />,
    )
    await userEvent.click(screen.getByRole("button", { name: /test connection/i }))
    expect(await screen.findByText("Connection failed")).toBeInTheDocument()
    expect(screen.getByText(/connection timeout/)).toBeInTheDocument()
  })

  it("validates before submitting and maps server field errors", async () => {
    const onSubmit = vi.fn().mockRejectedValue(
      Object.assign(new (await import("@/lib/api")).ApiError(422, "validation_failed", "Some fields are invalid.", { name: "A database with this name already exists." })),
    )
    renderWithProviders(<DatabaseForm mode="create" submitLabel="Save database" onSubmit={onSubmit} />)

    await userEvent.click(screen.getByRole("button", { name: "Save database" }))
    expect(await screen.findByText("Enter the host.")).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()

    await userEvent.type(screen.getByLabelText("Name"), "production")
    await userEvent.type(screen.getByLabelText("Host"), "db.internal")
    await userEvent.type(screen.getByLabelText("Username"), "app")
    await userEvent.type(screen.getByLabelText("Password"), "secret")
    await userEvent.click(screen.getByRole("button", { name: "Save database" }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce())
    expect(onSubmit.mock.calls[0][0]).toMatchObject({ name: "production", host: "db.internal", port: 5432, username: "app", password: "secret" })
    expect(await screen.findByText("A database with this name already exists.")).toBeInTheDocument()
  })
})
