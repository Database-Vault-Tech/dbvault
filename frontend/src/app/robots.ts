import type { MetadataRoute } from "next"

import { SITE_URL } from "@/lib/site"

export default function robots(): MetadataRoute.Robots {
  return {
    rules: [
      {
        userAgent: "*",
        allow: "/",
        // Everything behind sign-in is private and has no search value.
        disallow: ["/api/", "/dashboard", "/databases", "/backups", "/restore", "/storage", "/schedules", "/notifications", "/team", "/audit-log", "/settings", "/login", "/register", "/forgot-password", "/reset-password", "/invite"],
      },
    ],
    sitemap: `${SITE_URL}/sitemap.xml`,
    host: SITE_URL,
  }
}
