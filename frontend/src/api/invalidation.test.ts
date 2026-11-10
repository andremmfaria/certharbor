import { getConfigQueryKey, getSessionQueryKey } from '@andremmfaria/certharbor-client/react-query'
import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'
import spec from '../../../backend/openapi/openapi.json'
import { dropUserData, invalidate, operationsFor, TAGS } from './invalidation'

describe('invalidation map', () => {
  it('is derived from the spec: every operation lands under exactly its own tags', () => {
    for (const item of Object.values(spec.paths)) {
      for (const op of Object.values(item) as { operationId: string; tags: string[] }[]) {
        for (const tag of op.tags) {
          expect(operationsFor(tag as (typeof TAGS)[number])).toContain(op.operationId)
        }
      }
    }
  })

  it('uses only the fixed tag list', () => {
    const tags = new Set(
      Object.values(spec.paths).flatMap((item) =>
        Object.values(item).flatMap((op) => (op as { tags: string[] }).tags),
      ),
    )
    for (const tag of tags) expect(TAGS).toContain(tag)
  })

  it('invalidates the queries of the named tags and leaves the others', async () => {
    const queryClient = new QueryClient()
    queryClient.setQueryData(getSessionQueryKey(), { state: 'anonymous' })
    queryClient.setQueryData(getConfigQueryKey(), { x: 1 })

    await invalidate(queryClient, 'session')

    expect(queryClient.getQueryState(getSessionQueryKey())?.isInvalidated).toBe(true)
    expect(queryClient.getQueryState(getConfigQueryKey())?.isInvalidated).toBe(false)
  })

  it('drops user data but keeps the public queries', () => {
    const queryClient = new QueryClient()
    queryClient.setQueryData(getSessionQueryKey(), { state: 'anonymous' })
    queryClient.setQueryData(getConfigQueryKey(), { x: 1 })
    queryClient.setQueryData(['something-else'], 1)

    dropUserData(queryClient)

    expect(queryClient.getQueryData(getSessionQueryKey())).toBeDefined()
    expect(queryClient.getQueryData(getConfigQueryKey())).toBeDefined()
    expect(queryClient.getQueryData(['something-else'])).toBeUndefined()
  })
})
