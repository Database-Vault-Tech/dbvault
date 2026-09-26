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
    const testCall = calls.find((c) => c.method === "POST" && c.url === "/api/databases/test")
    expect(testCall?.body).toMatchObject({ host: "db.internal", port: 6543, database: "shop", username: "app", password: "pa$$word", ssl_mode: "require" })
  })

  it("asks SQLite for a file path only, and explains when SQLite isn't set up", async () => {
    const calls = mockFetch({
      "GET /api/database-engines": () => ({
        body: { data: [{ name: "sqlite", label: "SQLite", default_port: 0, capabilities: { file_based: true }, available: false, unavailable_reason: "Set SQLITE_ROOT first." }] },
      }),
      "POST /api/databases/test": () => ({ body: { data: { ok: false, message: "Set SQLITE_ROOT first.", tested_at: new Date().toISOString() } } }),
    })
    renderWithProviders(<DatabaseForm mode="create" submitLabel="Save database" onSubmit={vi.fn()} />)
    await userEvent.click(screen.getByRole("radio", { name: /SQLite/ }))

    expect(screen.queryByLabelText("Host")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Username")).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/connection string/i)).not.toBeInTheDocument()
    expect(await screen.findByText("SQLite isn't set up on this server yet")).toBeInTheDocument()

    await userEvent.type(screen.getByLabelText("Name"), "lite")
    await userEvent.type(screen.getByLabelText("Database file"), "../escape.db")
    await userEvent.click(screen.getByRole("button", { name: /test connection/i }))
    expect(await screen.findByText(/no leading \/, no \.\./)).toBeInTheDocument()

    await userEvent.clear(screen.getByLabelText("Database file"))
    await userEvent.type(screen.getByLabelText("Database file"), "myapp/app.db")
    await userEvent.click(screen.getByRole("button", { name: /test connection/i }))
    await waitFor(() => expect(calls.some((c) => c.url === "/api/databases/test")).toBe(true))
    expect(calls.find((c) => c.url === "/api/databases/test")?.body).toMatchObject({ engine: "sqlite", database: "myapp/app.db", host: "", port: 0, username: "" })
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

  it("switches engine defaults and submits a MySQL database", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    renderWithProviders(<DatabaseForm mode="create" submitLabel="Save database" onSubmit={onSubmit} />)

    await userEvent.click(screen.getByRole("radio", { name: /MySQL/ }))
    expect(screen.getByRole("radio", { name: /MySQL/ })).toHaveAttribute("aria-checked", "true")
    expect(screen.getByLabelText("Port")).toHaveValue(3306)
    // PostgreSQL's default database name is cleared for MySQL.
    expect(screen.getByLabelText("Database")).toHaveValue("")

    await userEvent.type(screen.getByLabelText("Name"), "shop")
    await userEvent.type(screen.getByLabelText("Host"), "mysql.internal")
    await userEvent.type(screen.getByLabelText("Database"), "shop")
    await userEvent.type(screen.getByLabelText("Username"), "app")
    await userEvent.type(screen.getByLabelText("Password"), "secret")
    await userEvent.click(screen.getByRole("button", { name: "Save database" }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce())
    expect(onSubmit.mock.calls[0][0]).toMatchObject({ engine: "mysql", port: 3306, database: "shop", ssl_mode: "prefer" })
  })

  it("fills the engine from a pasted MySQL connection string", () => {
    renderWithProviders(<DatabaseForm mode="create" submitLabel="Save database" onSubmit={vi.fn()} />)
    fireEvent.change(screen.getByLabelText(/connection string/i), { target: { value: "mariadb://app:pw@maria.internal/shop" } })
    expect(screen.getByRole("radio", { name: /MariaDB/ })).toHaveAttribute("aria-checked", "true")
    expect(screen.getByLabelText("Port")).toHaveValue(3306)
  })
})
