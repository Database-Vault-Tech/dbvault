// Streams /api/* to the DBVault Go API. API_URL is read at request time,
// so one frontend image works in any deployment. Request and response
// bodies are streamed (never buffered), so multi-gigabyte backup downloads
// pass straight through.

import type { NextRequest } from "next/server"

export const dynamic = "force-dynamic"

const API_URL = process.env.API_URL ?? "http://localhost:8080"

const FORWARD_REQUEST_HEADERS = [
  "accept",
  "authorization",
  "content-type",
  "cookie",
  "origin",
  "user-agent",
  "x-csrf-token",
  "x-dbvault-org",
  "x-request-id",
]

const DROP_RESPONSE_HEADERS = new Set(["connection", "keep-alive", "transfer-encoding", "content-encoding", "set-cookie"])

async function forward(req: NextRequest, ctx: { params: Promise<{ path: string[] }> }) {
  const { path } = await ctx.params
  const target = new URL(`/api/${path.map(encodeURIComponent).join("/")}`, API_URL)
  target.search = req.nextUrl.search

  const headers = new Headers()
  for (const name of FORWARD_REQUEST_HEADERS) {
    const v = req.headers.get(name)
    if (v) headers.set(name, v)
  }
  const clientIP = req.headers.get("x-forwarded-for")?.split(",")[0]?.trim() || req.headers.get("x-real-ip")
  if (clientIP) headers.set("x-forwarded-for", clientIP)

  const init: RequestInit & { duplex?: "half" } = { method: req.method, headers, redirect: "manual", cache: "no-store" }
  if (req.method !== "GET" && req.method !== "HEAD" && req.body) {
    init.body = req.body
    init.duplex = "half"
  }

  let upstream: Response
  try {
    upstream = await fetch(target, init)
  } catch {
    return Response.json(
      { error: { code: "api_unreachable", message: "The DBVault API is not reachable. Is the api service running?" } },
      { status: 502 },
    )
  }

  const out = new Headers()
  upstream.headers.forEach((value, key) => {
    if (!DROP_RESPONSE_HEADERS.has(key)) out.set(key, value)
  })
  for (const cookie of upstream.headers.getSetCookie()) out.append("set-cookie", cookie)
  return new Response(upstream.body, { status: upstream.status, statusText: upstream.statusText, headers: out })
}

export { forward as GET, forward as POST, forward as PATCH, forward as PUT, forward as DELETE }
