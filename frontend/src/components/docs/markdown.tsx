import { Link as LinkIcon } from "lucide-react"
import Link from "next/link"
import type { ComponentProps, ReactNode } from "react"
import ReactMarkdown, { type Components } from "react-markdown"
import rehypeSlug from "rehype-slug"
import remarkGfm from "remark-gfm"

import { resolveDocHref } from "@/lib/docs-nav"
import { cn } from "@/lib/utils"

import { Mermaid } from "./mermaid"

interface HastNode {
  type: string
  tagName?: string
  value?: string
  properties?: { className?: string[] }
  children?: HastNode[]
}

function text(node: HastNode | undefined): string {
  if (!node) return ""
  if (node.type === "text") return node.value ?? ""
  return (node.children ?? []).map(text).join("")
}

function heading(Tag: "h2" | "h3" | "h4", className: string) {
  function Heading({ id, children }: { id?: string; children?: ReactNode }) {
    return (
      <Tag id={id} className={cn("group scroll-mt-24 font-semibold tracking-tight", className)}>
        {children}
        {id && (
          <a href={`#${id}`} aria-label="Link to this section" className="ml-2 inline-flex align-middle text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 focus:opacity-100">
            <LinkIcon className="size-3.5" />
          </a>
        )}
      </Tag>
    )
  }
  return Heading
}

const components: Components = {
  h2: heading("h2", "mt-12 mb-4 border-b pb-2 text-xl"),
  h3: heading("h3", "mt-8 mb-3 text-lg"),
  h4: heading("h4", "mt-6 mb-2 text-base"),
  p: ({ children }) => <p className="my-4 leading-7">{children}</p>,
  a: ({ href = "", children }) => {
    const r = resolveDocHref(href)
    const cls = "font-medium text-brand underline-offset-4 hover:underline"
    return r.external ? (
      <a href={r.href} target="_blank" rel="noreferrer" className={cls}>
        {children}
      </a>
    ) : (
      <Link href={r.href} className={cls}>
        {children}
      </Link>
    )
  },
  ul: ({ children }) => <ul className="my-4 list-disc space-y-1.5 pl-6 marker:text-muted-foreground">{children}</ul>,
  ol: ({ children }) => <ol className="my-4 list-decimal space-y-1.5 pl-6 marker:text-muted-foreground">{children}</ol>,
  li: ({ children }) => <li className="pl-1 leading-7 [&>p]:my-1">{children}</li>,
  blockquote: ({ children }) => (
    <blockquote className="my-5 rounded-r-lg border-l-4 border-brand bg-brand/5 px-4 py-1 text-foreground [&>p]:my-2">{children}</blockquote>
  ),
  hr: () => <hr className="my-10" />,
  table: ({ children }) => (
    <div className="my-6 overflow-x-auto rounded-lg border">
      <table className="w-full border-collapse text-sm">{children}</table>
    </div>
  ),
  thead: ({ children }) => <thead className="bg-muted/50">{children}</thead>,
  th: ({ children, style }) => (
    <th style={style} className="border-b px-3 py-2 text-left font-medium whitespace-nowrap">
      {children}
    </th>
  ),
  td: ({ children, style }) => (
    <td style={style} className="border-b px-3 py-2 align-top [tr:last-child>&]:border-b-0">
      {children}
    </td>
  ),
  code: ({ className, children }) => {
    // Inline code; fenced blocks are handled by `pre` below.
    if (className) return <code className={className}>{children}</code>
    return <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-[0.85em] break-words">{children}</code>
  },
  pre: ({ node, children }) => {
    const code = (node as HastNode | undefined)?.children?.find((c) => c.tagName === "code")
    const lang = code?.properties?.className?.find((c) => c.startsWith("language-"))?.slice("language-".length)
    if (lang === "mermaid") return <Mermaid chart={text(code).trim()} />
    return <pre className="my-5 overflow-x-auto rounded-lg border bg-zinc-950 p-4 font-mono text-[13px] leading-6 text-zinc-100 dark:bg-zinc-900">{children}</pre>
  },
}

export function Markdown({ children, className, ...props }: { children: string } & Omit<ComponentProps<"div">, "children">) {
  return (
    <div className={cn("text-[15px] text-foreground/90", className)} {...props}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSlug]} components={components}>
        {children}
      </ReactMarkdown>
    </div>
  )
}
