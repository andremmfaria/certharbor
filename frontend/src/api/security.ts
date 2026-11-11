import {
  deleteIpBlockMutation,
  listAuditEventsOptions,
  listSecurityEventsOptions,
} from '@kantharos-ai/certharbor-client/react-query'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ListParams } from '../hooks/useListParams'
import { invalidate } from './invalidation'

// GET /api/v1/security/events: the filtered admin security view. q and
// outcome map straight onto the operation's own query params.
export function useSecurityEvents(
  params: Pick<ListParams, 'page' | 'pageSize'>,
  q: string | undefined,
  outcome: 'success' | 'failure' | undefined,
) {
  return useQuery(
    listSecurityEventsOptions({
      query: { page: params.page, pageSize: params.pageSize, q: q || undefined, outcome },
    }),
  )
}

// GET /api/v1/audit-events: the full unfiltered feed of the same table, no
// q/outcome filter controls.
export function useAuditEvents(params: Pick<ListParams, 'page' | 'pageSize'>) {
  return useQuery(listAuditEventsOptions({ query: { page: params.page, pageSize: params.pageSize } }))
}

// DELETE /api/v1/security/ip-blocks/{ip}. Invalidates 'security' so both
// pages' lists (and any blocked-IP indicator on the user profile) refetch.
export function useUnblockIP() {
  const queryClient = useQueryClient()
  return useMutation({
    ...deleteIpBlockMutation(),
    onSuccess: () => invalidate(queryClient, 'security'),
  })
}
