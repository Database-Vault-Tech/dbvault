import type { MetadataRoute } from "next"

import { DOCS } from "@/lib/docs-nav"
import { SITE_URL } from "@/lib/site"

export default function sitemap(): MetadataRoute.Sitemap {
  return [
    {
      url: `${SITE_URL}/`,
      lastModified: new Date(),
      changeFrequency: "weekly",
      priority: 1,
    },
    { url: `${SITE_URL}/docs`, lastModified: new Date(), changeFrequency: "weekly", priority: 0.8 },
    ...DOCS.map((d) => ({ url: `${SITE_URL}/docs/${d.slug}`, lastModified: new Date(), changeFrequency: "weekly" as const, priority: 0.7 })),
  ]
}
