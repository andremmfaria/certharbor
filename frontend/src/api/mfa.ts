import {
  confirmMfaEnrollmentMutation,
  disableMfaMutation,
  getMfaEnrollmentQrOptions,
  getMfaOptions,
  startMfaEnrollmentMutation,
} from '@kantharos-ai/certharbor-client/react-query'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { invalidate } from './invalidation'

export function useMfaStatus() {
  return useQuery(getMfaOptions())
}

export function useStartMfaEnrollment() {
  const queryClient = useQueryClient()
  return useMutation({
    ...startMfaEnrollmentMutation(),
    onSuccess: () => invalidate(queryClient, 'profile'),
  })
}

// Fetched only once enrollment is pending (`enabled` gates the request), and
// never cached across a fresh enrollment: starting over mints a new secret,
// so a stale QR for the old one must not be served from the query cache.
export function useMfaEnrollmentQr(enabled: boolean) {
  return useQuery({ ...getMfaEnrollmentQrOptions(), enabled, staleTime: 0, gcTime: 0, retry: false })
}

export function useConfirmMfaEnrollment() {
  const queryClient = useQueryClient()
  return useMutation({
    ...confirmMfaEnrollmentMutation(),
    onSuccess: () => invalidate(queryClient, 'profile'),
  })
}

export function useDisableMfa() {
  const queryClient = useQueryClient()
  return useMutation({
    ...disableMfaMutation(),
    onSuccess: () => invalidate(queryClient, 'profile'),
  })
}
