import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render } from "@testing-library/react"
import type { ReactElement } from "react"
import { vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"

export function renderWithProviders(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>{ui}</TooltipProvider>
    </QueryClientProvider>,
  )
}

export interface MockCall {
  url: string
  method: string
  headers: Record<string, string>
  body: unknown
}

/** Replaces fetch with a router keyed by "METHOD /path". */
export function mockFetch(routes: Record<string, (body: unknown) => { status?: number; body?: unknown }>) {
  const calls: MockCall[] = []
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
    const method = (init?.method ?? "GET").toUpperCase()
    const headers = Object.fromEntries(Object.entries((init?.headers ?? {}) as Record<string, string>))
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ url, method, headers, body })
    const path = url.split("?")[0]
    const handler = routes[`${method} ${path}`]
    if (!handler) return new Response(JSON.stringify({ error: { code: "not_found", message: `no mock for ${method} ${path}` } }), { status: 404 })
    const res = handler(body)
    const status = res.status ?? 200
    return new Response(status === 204 ? null : JSON.stringify(res.body ?? {}), { status, headers: { "Content-Type": "application/json" } })
  })
  return calls
}
