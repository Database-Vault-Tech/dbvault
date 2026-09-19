import { describe, expect, it } from "vitest"

import { mockFetch } from "@/test/utils"

import { api, ApiError, ORG_STORAGE_KEY } from "./api"

describe("api client", () => {
  it("sends the CSRF token and organization on mutations", async () => {
    document.cookie = "dbvault_csrf=csrf-123; path=/"
    window.localStorage.setItem(ORG_STORAGE_KEY, "org-1")
    const calls = mockFetch({ "POST /api/backups": () => ({ status: 202, body: { data: { job_id: "j1", backup_id: "b1", status: "queued" } } }) })
    const res = await api.post<{ job_id: string }>("/backups", { database_id: "d1" })
    expect(res.job_id).toBe("j1")
    expect(calls[0].headers["X-CSRF-Token"]).toBe("csrf-123")
    expect(calls[0].headers["X-DBVault-Org"]).toBe("org-1")
    expect(calls[0].headers["Content-Type"]).toBe("application/json")
    expect(calls[0].body).toEqual({ database_id: "d1" })
  })

  it("does not send CSRF on GET and can skip the org header", async () => {
    document.cookie = "dbvault_csrf=csrf-123; path=/"
    window.localStorage.setItem(ORG_STORAGE_KEY, "org-1")
    const calls = mockFetch({ "GET /api/me": () => ({ body: { data: { user: { id: "u" } } } }) })
    await api.get("/me", { noOrg: true })
    expect(calls[0].headers["X-CSRF-Token"]).toBeUndefined()
    expect(calls[0].headers["X-DBVault-Org"]).toBeUndefined()
  })

  it("turns error envelopes into ApiError with field errors", async () => {
    mockFetch({
      "POST /api/databases": () => ({
        status: 422,
        body: { error: { code: "validation_failed", message: "Some fields are invalid.", fields: { name: "Taken." }, request_id: "r1" } },
      }),
    })
    const err = (await api.post("/databases", {}).catch((e: unknown) => e)) as ApiError
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(422)
    expect(err.fields).toEqual({ name: "Taken." })
    expect(err.requestId).toBe("r1")
  })

  it("returns undefined for 204 responses", async () => {
    mockFetch({ "DELETE /api/backups/b1": () => ({ status: 204 }) })
    await expect(api.delete("/backups/b1")).resolves.toBeUndefined()
  })
})
