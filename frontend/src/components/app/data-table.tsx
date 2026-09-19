"use client"

import type { InfiniteData, UseInfiniteQueryResult } from "@tanstack/react-query"
import { ChevronLeft, ChevronRight, Search, X } from "lucide-react"
import { useMemo, useState, type ReactNode } from "react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { cn } from "@/lib/utils"

export const ALL = "all"
export const PAGE_SIZES = [10, 25, 50, 100]

/** Filter row above a table. */
export function TableToolbar({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("mb-4 flex flex-wrap items-center gap-2", className)}>{children}</div>
}

export function TableSearch({
  value,
  onChange,
  placeholder = "Search…",
  label = "Search",
  className,
}: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
  label?: string
  className?: string
}) {
  return (
    <div className={cn("relative w-full sm:w-64", className)}>
      <Search aria-hidden className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
      <Input aria-label={label} value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} className="pr-7 pl-8" />
      {value && (
        <button
          type="button"
          aria-label="Clear search"
          onClick={() => onChange("")}
          className="absolute top-1/2 right-1.5 flex size-5 -translate-y-1/2 items-center justify-center rounded text-muted-foreground hover:text-foreground"
        >
          <X className="size-3.5" />
        </button>
      )}
    </div>
  )
}

/** Select whose first option means "no filter". */
export function FilterSelect({
  value,
  onChange,
  label,
  allLabel,
  options,
  className = "w-40",
}: {
  value: string
  onChange: (v: string) => void
  label: string
  allLabel: string
  options: { value: string; label: string }[]
  className?: string
}) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className={className} aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>{allLabel}</SelectItem>
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

export function ClearFiltersButton({ show, onClear }: { show: boolean; onClear: () => void }) {
  if (!show) return null
  return (
    <Button variant="ghost" onClick={onClear}>
      Clear filters
    </Button>
  )
}

/** Page controls shared by every table. */
export function TablePagination({
  start,
  end,
  total,
  hasMore,
  canPrev,
  onPrev,
  onNext,
  pageSize,
  onPageSizeChange,
  loading,
  noun = "rows",
}: {
  start: number
  end: number
  total: number
  /** More rows exist on the server beyond `total` loaded so far. */
  hasMore: boolean
  canPrev: boolean
  onPrev: () => void
  onNext: () => void
  pageSize: number
  onPageSizeChange: (n: number) => void
  loading?: boolean
  noun?: string
}) {
  const canNext = hasMore || end < total
  // One short page: the count alone is enough, no controls to click.
  const onePage = !canNext && !canPrev
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 px-1 text-sm text-muted-foreground">
      <span aria-live="polite" className="tabular">
        {total === 0 ? `No ${noun}` : `Showing ${start + 1}–${end} of ${total}${hasMore ? "+" : ""} ${noun}`}
      </span>
      <div className={cn("flex items-center gap-2", onePage && total <= PAGE_SIZES[0] && "hidden")}>
        <Select value={String(pageSize)} onValueChange={(v) => onPageSizeChange(Number(v))}>
          <SelectTrigger className="w-28" aria-label="Rows per page" size="sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PAGE_SIZES.map((n) => (
              <SelectItem key={n} value={String(n)}>
                {n} / page
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button variant="outline" size="sm" onClick={onPrev} disabled={!canPrev} aria-label="Previous page">
          <ChevronLeft /> Prev
        </Button>
        <Button variant="outline" size="sm" onClick={onNext} disabled={!canNext || loading} aria-label="Next page">
          {loading ? <Spinner /> : null} Next <ChevronRight />
        </Button>
      </div>
    </div>
  )
}

/** Page index that resets to the first page when the filters or page size change. */
function usePageState(resetKey: unknown, pageSize: number): [number, (p: number) => void] {
  const [state, setState] = useState({ page: 0, key: "" })
  const key = `${JSON.stringify(resetKey)}|${pageSize}`
  const page = state.key === key ? state.page : 0
  return [page, (p: number) => setState({ page: p, key })]
}

/** Client-side paging for lists the API returns in full. */
export function usePaging<T>(rows: T[], resetKey: unknown = "") {
  const [pageSize, setPageSize] = useState(PAGE_SIZES[1])
  const [page, setPage] = usePageState(resetKey, pageSize)
  const pageCount = Math.max(1, Math.ceil(rows.length / pageSize))
  const current = Math.min(page, pageCount - 1)
  const start = current * pageSize
  const pageRows = useMemo(() => rows.slice(start, start + pageSize), [rows, start, pageSize])
  return {
    rows: pageRows,
    props: {
      start,
      end: Math.min(start + pageSize, rows.length),
      total: rows.length,
      hasMore: false,
      canPrev: current > 0,
      onPrev: () => setPage(current - 1),
      onNext: () => setPage(current + 1),
      pageSize,
      onPageSizeChange: setPageSize,
    },
  }
}

type PagedQuery<T> = UseInfiniteQueryResult<InfiniteData<{ data: T[] }>, unknown>

/**
 * Paging for cursor-paginated endpoints: pages through what's loaded and
 * fetches the next cursor page when the reader runs past it.
 */
export function useCursorPaging<T>(query: PagedQuery<T>, resetKey: unknown = "") {
  const [pageSize, setPageSize] = useState(PAGE_SIZES[1])
  const [page, setPage] = usePageState(resetKey, pageSize)
  const rows = useMemo(() => query.data?.pages.flatMap((p) => p.data) ?? [], [query.data])
  const pageCount = Math.max(1, Math.ceil(rows.length / pageSize))
  const current = Math.min(page, pageCount - 1)
  const start = current * pageSize
  const end = Math.min(start + pageSize, rows.length)

  const next = async () => {
    // Fetch the next cursor page when the coming page isn't loaded yet.
    if (start + pageSize >= rows.length && query.hasNextPage) {
      await query.fetchNextPage()
    }
    setPage(current + 1)
  }

  return {
    rows: useMemo(() => rows.slice(start, start + pageSize), [rows, start, pageSize]),
    props: {
      start,
      end,
      total: rows.length,
      hasMore: !!query.hasNextPage,
      canPrev: current > 0,
      onPrev: () => setPage(current - 1),
      onNext: next,
      pageSize,
      onPageSizeChange: setPageSize,
      loading: query.isFetchingNextPage,
    },
  }
}

/**
 * Paging over rows already filtered in the browser, backed by a
 * cursor-paginated query: "Next" past the last loaded row fetches more.
 */
export function useSearchPaging<T>(rows: T[], query: PagedQuery<unknown>, resetKey: unknown = "") {
  const paging = usePaging(rows, resetKey)
  const { end, total, onNext } = paging.props
  return {
    rows: paging.rows,
    props: {
      ...paging.props,
      hasMore: !!query.hasNextPage,
      loading: query.isFetchingNextPage,
      onNext: async () => {
        if (end >= total && query.hasNextPage) await query.fetchNextPage()
        onNext()
      },
    },
  }
}

/** Case-insensitive "does any field contain the query" match. */
export function matches(query: string, ...fields: (string | number | null | undefined)[]) {
  const q = query.trim().toLowerCase()
  if (!q) return true
  return fields.some((f) => f != null && String(f).toLowerCase().includes(q))
}
