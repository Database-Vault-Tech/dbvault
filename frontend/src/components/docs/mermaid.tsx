"use client"

import { useTheme } from "next-themes"
import { useEffect, useId, useState } from "react"

/** Renders a Mermaid diagram from the docs; falls back to its source if it can't. */
export function Mermaid({ chart }: { chart: string }) {
  const { resolvedTheme } = useTheme()
  const id = "mermaid-" + useId().replace(/[^a-zA-Z0-9]/g, "")
  const [svg, setSvg] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let cancelled = false
    import("mermaid")
      .then(async ({ default: mermaid }) => {
        mermaid.initialize({
          startOnLoad: false,
          // Output is sanitized; diagrams can't run scripts or add click handlers.
          securityLevel: "strict",
          theme: resolvedTheme === "dark" ? "dark" : "neutral",
          fontFamily: "inherit",
        })
        const { svg } = await mermaid.render(id, chart)
        if (!cancelled) setSvg(svg)
      })
      .catch(() => !cancelled && setFailed(true))
    return () => {
      cancelled = true
    }
  }, [chart, id, resolvedTheme])

  if (failed) {
    return <pre className="my-5 overflow-x-auto rounded-lg border bg-muted/50 p-4 font-mono text-xs leading-5">{chart}</pre>
  }
  return (
    <figure
      className="my-6 flex min-h-24 justify-center overflow-x-auto rounded-lg border bg-card p-4 [&_svg]:h-auto [&_svg]:max-w-full"
      aria-label="Diagram"
      // Mermaid's own sanitized SVG output (securityLevel: strict).
      dangerouslySetInnerHTML={svg ? { __html: svg } : undefined}
    />
  )
}
