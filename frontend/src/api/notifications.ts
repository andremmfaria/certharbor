import {
  getNotificationsOptions,
  patchNotificationsMutation,
  testNotificationsMutation,
} from '@kantharos-ai/certharbor-client/react-query'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { invalidate } from './invalidation'

// GET /api/v1/admin/notifications. smtpPassword is writeOnly and never
// present in this response (5.8), so the form field it backs starts blank
// on every load and a save that omits it leaves the stored value unchanged.
export function useNotificationSettings() {
  return useQuery(getNotificationsOptions())
}

// PATCH /api/v1/admin/notifications is merge-style: absent fields are
// unchanged, so the form only sends smtpPassword when the admin typed a
// new one.
export function usePatchNotifications() {
  const queryClient = useQueryClient()
  return useMutation({
    ...patchNotificationsMutation(),
    onSuccess: () => invalidate(queryClient, 'admin'),
  })
}

// POST /api/v1/admin/notifications/test dispatches one webhook test event
// against the currently saved settings; it takes no body.
export function useTestNotifications() {
  return useMutation({ ...testNotificationsMutation() })
}
