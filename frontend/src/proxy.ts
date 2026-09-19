import { NextResponse, type NextRequest } from "next/server"

// Optimistic auth check: app routes without a session cookie go straight to
// the login page. Real authorization is always enforced by the API.
const APP_PREFIXES = [
  "/dashboard",
  "/databases",
  "/backups",
  "/restore",
  "/storage",
  "/schedules",
  "/notifications",
  "/audit-logs",
  "/settings",
]

export function proxy(request: NextRequest) {
  const { pathname, search } = request.nextUrl
  if (APP_PREFIXES.some((p) => pathname === p || pathname.startsWith(p + "/")) && !request.cookies.has("dbvault_session")) {
    const url = request.nextUrl.clone()
    url.pathname = "/login"
    url.search = `?next=${encodeURIComponent(pathname + search)}`
    return NextResponse.redirect(url)
  }
  return NextResponse.next()
}

export const config = {
  matcher: [
    "/dashboard/:path*",
    "/databases/:path*",
    "/backups/:path*",
    "/restore/:path*",
    "/storage/:path*",
    "/schedules/:path*",
    "/notifications/:path*",
    "/audit-logs/:path*",
    "/settings/:path*",
  ],
}
