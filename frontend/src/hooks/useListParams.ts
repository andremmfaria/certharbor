import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router'

export const DEFAULT_PAGE_SIZE = 25
export const SEARCH_DEBOUNCE_MS = 300

export interface ListParams {
  page: number
  pageSize: number
  q: string
  sort: string | undefined
  order: 'asc' | 'desc' | undefined
}

function positiveInt(raw: string | null, fallback: number): number {
  const n = Number(raw)
  return Number.isInteger(n) && n > 0 ? n : fallback
}

// The URL search string is the single source of truth for table state, so a
// filtered, paged, sorted table is deep-linkable for the same reason a route
// is. Defaults are omitted from the address to keep it short.
export function useListParams() {
  const [search, setSearch] = useSearchParams()

  const params: ListParams = {
    page: positiveInt(search.get('page'), 1),
    pageSize: positiveInt(search.get('pageSize'), DEFAULT_PAGE_SIZE),
    q: search.get('q') ?? '',
    sort: search.get('sort') ?? undefined,
    order: search.get('order') === 'desc' ? 'desc' : search.get('order') === 'asc' ? 'asc' : undefined,
  }

  const update = useCallback(
    (patch: Record<string, string | undefined>) =>
      setSearch(
        (current) => {
          const next = new URLSearchParams(current)
          for (const [key, value] of Object.entries(patch)) {
            if (value === undefined || value === '') next.delete(key)
            else next.set(key, value)
          }
          return next
        },
        { replace: true },
      ),
    [setSearch],
  )

  // The input is controlled locally and committed to the URL after a pause, so
  // typing does not issue a request per keystroke. A new query resets paging.
  const [qInput, setQInput] = useState(params.q)
  const committed = useRef(params.q)
  useEffect(() => {
    if (qInput === committed.current) return
    const timer = setTimeout(() => {
      committed.current = qInput
      update({ q: qInput, page: undefined })
    }, SEARCH_DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [qInput, update])

  return {
    params,
    qInput,
    setQuery: setQInput,
    setPage: (page: number) => update({ page: page > 1 ? String(page) : undefined }),
    setPageSize: (size: number) =>
      update({ pageSize: size === DEFAULT_PAGE_SIZE ? undefined : String(size), page: undefined }),
    setSort: (sort: string, order: 'asc' | 'desc') => update({ sort, order, page: undefined }),
  }
}
