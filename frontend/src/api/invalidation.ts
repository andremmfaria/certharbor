import type { QueryClient } from '@tanstack/react-query'
import operationsByTag from 'virtual:operation-tags'

export const TAGS = [
  'session',
  'certificates',
  'ca',
  'acme',
  'provisioners',
  'security',
  'admin',
  'users',
  'profile',
  'system',
  'dashboard',
] as const

export type Tag = (typeof TAGS)[number]

export function operationIdOf(queryKey: readonly unknown[]): string | undefined {
  const head = queryKey[0]
  if (typeof head !== 'object' || head === null || !('_id' in head)) return undefined
  return typeof head._id === 'string' ? head._id : undefined
}

// The generated query keys carry the operationId, and the spec carries each
// operation's tags, so a tag resolves to a predicate with nothing hand-assembled.
export function invalidate(queryClient: QueryClient, ...tags: Tag[]): Promise<void> {
  const ids = new Set(tags.flatMap((tag) => operationsByTag[tag] ?? []))
  return queryClient.invalidateQueries({
    predicate: (query) => {
      const id = operationIdOf(query.queryKey)
      return id !== undefined && ids.has(id)
    },
  })
}

export function operationsFor(tag: Tag): readonly string[] {
  return operationsByTag[tag] ?? []
}

const PUBLIC_OPERATIONS = new Set(['getSession', 'getConfig'])

// Everything cached on behalf of the signed-in user. The boot session and the
// public config survive: the gate needs the first to re-evaluate, and the
// second belongs to no user.
export function dropUserData(queryClient: QueryClient): void {
  queryClient.removeQueries({
    predicate: (query) => {
      const id = operationIdOf(query.queryKey)
      return id === undefined || !PUBLIC_OPERATIONS.has(id)
    },
  })
}
