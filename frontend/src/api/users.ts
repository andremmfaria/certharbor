import {
  createTemporaryUserMutation,
  createUserMutation,
  deleteTemporaryUserMutation,
  deleteUserMutation,
  listTemporaryUsersOptions,
  listUsersOptions,
  resetUserPasswordMutation,
  updateUserMutation,
} from '@kantharos-ai/certharbor-client/react-query'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ListParams } from '../hooks/useListParams'
import { invalidate } from './invalidation'

// GET /api/v1/users: the admin user list. q maps straight onto the
// operation's own query param, same shape as useSecurityEvents.
export function useUsers(params: Pick<ListParams, 'page' | 'pageSize'>, q: string | undefined) {
  return useQuery(
    listUsersOptions({ query: { page: params.page, pageSize: params.pageSize, q: q || undefined } }),
  )
}

// POST /api/v1/users.
export function useCreateUser() {
  const queryClient = useQueryClient()
  return useMutation({ ...createUserMutation(), onSuccess: () => invalidate(queryClient, 'users') })
}

// PATCH /api/v1/users/{id}: covers both role and active-state changes.
export function useUpdateUser() {
  const queryClient = useQueryClient()
  return useMutation({ ...updateUserMutation(), onSuccess: () => invalidate(queryClient, 'users') })
}

// DELETE /api/v1/users/{id}.
export function useDeleteUser() {
  const queryClient = useQueryClient()
  return useMutation({ ...deleteUserMutation(), onSuccess: () => invalidate(queryClient, 'users') })
}

// POST /api/v1/users/{id}/password-reset. No list invalidation: it changes
// no field the list table renders.
export function useResetUserPassword() {
  return useMutation(resetUserPasswordMutation())
}

// GET /api/v1/temporary-users: unpaginated, operator-sized table (mirrors
// useProvisioners).
export function useTemporaryUsers() {
  return useQuery(listTemporaryUsersOptions())
}

// POST /api/v1/temporary-users. The response carries the one-time
// passphrase; the caller reads it from the mutation result rather than a
// refetch, since it is never shown again.
export function useCreateTemporaryUser() {
  const queryClient = useQueryClient()
  return useMutation({ ...createTemporaryUserMutation(), onSuccess: () => invalidate(queryClient, 'users') })
}

// DELETE /api/v1/temporary-users/{id}.
export function useDeleteTemporaryUser() {
  const queryClient = useQueryClient()
  return useMutation({
    ...deleteTemporaryUserMutation(),
    onSuccess: () => invalidate(queryClient, 'users'),
  })
}
