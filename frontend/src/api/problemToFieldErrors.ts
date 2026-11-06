import type { Problem } from './errors'

const FIELD_PREFIX = 'body.'

// 'body.items[3].name' becomes 'items.3.name', the path react-hook-form uses.
// Locations outside the body (query, path, header) are not a form field and
// are skipped.
function toFieldPath(location: string): string | undefined {
  if (!location.startsWith(FIELD_PREFIX)) return undefined
  return location.slice(FIELD_PREFIX.length).replace(/\[(\d+)\]/g, '.$1')
}

export function problemToFieldErrors(problem: Problem): Record<string, string> {
  const fields: Record<string, string> = {}
  for (const detail of problem.errors ?? []) {
    if (!detail?.location) continue
    const path = toFieldPath(detail.location)
    if (path && !(path in fields)) fields[path] = detail.message ?? 'Invalid value'
  }
  return fields
}
