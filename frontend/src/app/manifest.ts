import type { MetadataRoute } from "next"

import { SITE_DESCRIPTION, SITE_NAME, SITE_TAGLINE } from "@/lib/site"

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: `${SITE_NAME} — ${SITE_TAGLINE}`,
    short_name: SITE_NAME,
    description: SITE_DESCRIPTION,
    start_url: "/dashboard",
    display: "standalone",
    background_color: "#0e0f12",
    theme_color: "#0e0f12",
    icons: [{ src: "/icon.svg", sizes: "any", type: "image/svg+xml" }],
  }
}
