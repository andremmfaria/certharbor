import {
  confirmPasswordResetMutation,
  createSessionMutation,
  deleteSessionMutation,
  getConfigOptions,
  getSessionOptions,
  getSessionQueryKey,
  requestPasswordResetMutation,
  submitMfaMutation,
  validatePasswordResetMutation,
} from '@kantharos-ai/certharbor-client/react-query'
import type { SessionBody } from '@kantharos-ai/certharbor-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { isNetworkOrServerError } from './errors'
import { dropUserData } from './invalidation'
import { clearSessionExpiry } from './sessionExpiry'

// The boot call. A 4xx is never retried and a 5xx or a refused connection is,
// because the backend being down (5.10) must reach the unavailable branch
// rather than the login form, and a retry of a 503 cannot race the CSRF
// pairing the way two anonymous 2xx responses can (5.4).
export function sessionQueryOptions() {
  return {
    ...getSessionOptions(),
    retry: (failureCount: number, error: unknown) => failureCount < 3 && isNetworkOrServerError(error),
    retryDelay: (attempt: number) => 500 * 2 ** attempt,
  }
}

export function useSession() {
  return useQuery(sessionQueryOptions())
}

// Gated on the boot call settling so no other request runs before the one
// that mints the CSRF pair (5.4).
export function useConfig(enabled: boolean) {
  return useQuery({ ...getConfigOptions(), enabled })
}

export function useCreateSession() {
  const queryClient = useQueryClient()
  return useMutation({
    ...createSessionMutation(),
    onSuccess: (session: SessionBody) => queryClient.setQueryData(getSessionQueryKey(), session),
  })
}

export function useSubmitMfa() {
  const queryClient = useQueryClient()
  return useMutation({
    ...submitMfaMutation(),
    onSuccess: (session: SessionBody) => queryClient.setQueryData(getSessionQueryKey(), session),
  })
}

// Unauthenticated like useCreateSession/useSubmitMfa above, but with no
// session to seed on success: the caller never has a session yet, and
// requestPasswordReset's 202 carries no body at all (no-enumeration, 5.9).
export function useRequestPasswordReset() {
  return useMutation(requestPasswordResetMutation())
}

export function useValidatePasswordReset() {
  return useMutation(validatePasswordResetMutation())
}

export function useConfirmPasswordReset() {
  return useMutation(confirmPasswordResetMutation())
}

// Logging out drops every cached query but the public ones, because none of
// them belongs to the next person to sign in on this tab. The session is
// refetched rather than assumed so the CSRF pair, which logout expires, is
// re-minted for the anonymous caller.
export function useLogout() {
  const queryClient = useQueryClient()
  return useMutation({
    ...deleteSessionMutation(),
    onSettled: async () => {
      dropUserData(queryClient)
      clearSessionExpiry()
      await queryClient.fetchQuery({ ...sessionQueryOptions(), staleTime: 0 }).catch(() => undefined)
    },
  })
}
