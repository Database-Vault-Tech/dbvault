import { ImageResponse } from "next/og"

import { SITE_NAME, SITE_TAGLINE } from "@/lib/site"

export const alt = `${SITE_NAME} — ${SITE_TAGLINE}`
export const size = { width: 1200, height: 630 }
export const contentType = "image/png"

/** Social card: dark canvas, green mark, the promise, the engines. */
export default function OpenGraphImage() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          background: "linear-gradient(135deg, #0e0f12 0%, #10161a 55%, #0e1a16 100%)",
          padding: 72,
          color: "#f5f7f8",
          fontFamily: "sans-serif",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 20 }}>
          <div
            style={{
              width: 64,
              height: 64,
              borderRadius: 18,
              border: "4px solid #34d399",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
            }}
          >
            <div style={{ width: 24, height: 24, borderRadius: 8, background: "#34d399" }} />
          </div>
          <div style={{ fontSize: 44, fontWeight: 700, letterSpacing: -1 }}>{SITE_NAME}</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 24 }}>
          <div style={{ fontSize: 74, fontWeight: 700, lineHeight: 1.05, letterSpacing: -2.5, maxWidth: 940 }}>
            A backup you haven&apos;t restored is just a hope.
          </div>
          <div style={{ fontSize: 32, color: "#9fb0ad", maxWidth: 900, lineHeight: 1.35 }}>
            Automated, encrypted, restore-tested backups for PostgreSQL, MySQL and MariaDB. Self-hosted, Apache-2.0.
          </div>
        </div>
        <div style={{ display: "flex", gap: 14, fontSize: 24, color: "#34d399" }}>
          {["PostgreSQL", "MySQL", "MariaDB", "S3 · R2 · MinIO"].map((t) => (
            <div key={t} style={{ display: "flex", border: "1px solid #2b3b36", background: "#131a18", borderRadius: 999, padding: "10px 22px" }}>
              {t}
            </div>
          ))}
        </div>
      </div>
    ),
    size,
  )
}
