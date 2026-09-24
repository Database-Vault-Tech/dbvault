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

// The marketing landing page is only for the public site (dbvault.tech). A
// self-hosted install opens straight on the app unless LANDING_PAGE=true.
// Read per request, so it can be flipped without rebuilding the image.
function landingPageEnabled() {
  return ["1", "true", "yes", "on"].includes((process.env.LANDING_PAGE ?? "").trim().toLowerCase())
}

export function proxy(request: NextRequest) {
  const { pathname, search } = request.nextUrl
  const signedIn = request.cookies.has("dbvault_session")
  if (pathname === "/") {
    if (landingPageEnabled()) return NextResponse.next()
    const url = request.nextUrl.clone()
    url.pathname = signedIn ? "/dashboard" : "/login"
    url.search = ""
    return NextResponse.redirect(url)
  }
  if (APP_PREFIXES.some((p) => pathname === p || pathname.startsWith(p + "/")) && !signedIn) {
    const url = request.nextUrl.clone()
    url.pathname = "/login"
    url.search = `?next=${encodeURIComponent(pathname + search)}`
    return NextResponse.redirect(url)
  }
  return NextResponse.next()
}

export const config = {
  matcher: [
    "/",
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
