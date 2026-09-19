// Browser client for the DBVault API.
//
// All requests go to same-origin /api/*, which the Next.js route handler
// streams to the Go API. Session cookies are HttpOnly; the CSRF token is read
// from its (readable) cookie and echoed in X-CSRF-Token on every mutation.

import type { ListMeta } from "./types"

export const ORG_STORAGE_KEY = "dbvault.org"
const CSRF_COOKIE = "dbvault_csrf"

export class ApiError extends Error {
  status: number
  code: string
  fields: Record<string, string>
  requestId?: string

  constructor(status: number, code: string, message: string, fields: Record<string, string> = {}, requestId?: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.code = code
    this.fields = fields
    this.requestId = requestId
  }
}

function readCookie(name: string): string | undefined {
  if (typeof document === "undefined") return undefined
  const match = document.cookie.split("; ").find((c) => c.startsWith(name + "="))
  return match ? decodeURIComponent(match.slice(name.length + 1)) : undefined
}

export function currentOrgId(): string | undefined {
  if (typeof window === "undefined") return undefined
  try {
    return window.localStorage.getItem(ORG_STORAGE_KEY) ?? undefined
  } catch {
    return undefined
  }
}

type Method = "GET" | "POST" | "PATCH" | "PUT" | "DELETE"

interface RequestOptions {
  method?: Method
  body?: unknown
  signal?: AbortSignal
  /** Skip the X-DBVault-Org header (account-level endpoints). */
  noOrg?: boolean
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<{ data: T; meta?: ListMeta }> {
  const method = opts.method ?? "GET"
  const headers: Record<string, string> = { Accept: "application/json" }
  if (opts.body !== undefined) headers["Content-Type"] = "application/json"
  if (method !== "GET") {
    const csrf = readCookie(CSRF_COOKIE)
    if (csrf) headers["X-CSRF-Token"] = csrf
  }
  const org = opts.noOrg ? undefined : currentOrgId()
  if (org) headers["X-DBVault-Org"] = org

  let res: Response
  try {
    res = await fetch(`/api${path}`, {
      method,
      headers,
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      credentials: "same-origin",
      signal: opts.signal,
      cache: "no-store",
    })
  } catch (err) {
    if ((err as Error).name === "AbortError") throw err
    throw new ApiError(0, "network_error", "Could not reach the DBVault API. Check your connection and try again.")
  }

  if (res.status === 204) return { data: undefined as T }
  const json = await res.json().catch(() => null)
  if (!res.ok) {
    const e = json?.error
    throw new ApiError(
      res.status,
      e?.code ?? "http_error",
      e?.message ?? `Request failed with status ${res.status}`,
      e?.fields ?? {},
      e?.request_id,
    )
  }
  return { data: json?.data as T, meta: json?.meta }
}

export const api = {
  get: <T>(path: string, opts?: Omit<RequestOptions, "method" | "body">) => request<T>(path, opts).then((r) => r.data),
  list: <T>(path: string, opts?: Omit<RequestOptions, "method" | "body">) => request<T[]>(path, opts),
  post: <T>(path: string, body?: unknown, opts?: Omit<RequestOptions, "method" | "body">) =>
    request<T>(path, { ...opts, method: "POST", body: body ?? {} }).then((r) => r.data),
  patch: <T>(path: string, body: unknown, opts?: Omit<RequestOptions, "method" | "body">) =>
    request<T>(path, { ...opts, method: "PATCH", body }).then((r) => r.data),
  delete: <T = void>(path: string, opts?: Omit<RequestOptions, "method" | "body">) =>
    request<T>(path, { ...opts, method: "DELETE" }).then((r) => r.data),
}

/** Human-friendly message for any thrown value. */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return "Something went wrong."
}

/** Field errors from a validation failure (for react-hook-form). */
export function fieldErrors(err: unknown): Record<string, string> {
  return err instanceof ApiError ? err.fields : {}
}

/** URL for streaming a backup download through the same-origin proxy. */
export function backupDownloadUrl(id: string, format: "raw" | "dump" = "raw"): string {
  return `/api/backups/${id}/download?format=${format}`
}
