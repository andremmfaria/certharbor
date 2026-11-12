import {
  changePasswordMutation,
  getProfileOptions,
  patchProfileMutation,
  patchProfilePreferencesMutation,
} from '@kantharos-ai/certharbor-client/react-query'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { invalidate } from './invalidation'

// GET /api/v1/profile: the signed-in user's own account. No id anywhere in
// this module (5.5) — the server derives the subject from the session.
export function useProfile() {
  return useQuery(getProfileOptions())
}

// PATCH /api/v1/profile is merge-style (5.8): display name and email.
export function usePatchProfile() {
  const queryClient = useQueryClient()
  return useMutation({
    ...patchProfileMutation(),
    onSuccess: () => invalidate(queryClient, 'profile'),
  })
}

// PATCH /api/v1/profile/preferences is merge-style (5.8): the theme toggle.
export function usePatchProfilePreferences() {
  const queryClient = useQueryClient()
  return useMutation({
    ...patchProfilePreferencesMutation(),
    onSuccess: () => invalidate(queryClient, 'profile'),
  })
}

// POST /api/v1/profile/password requires the current password (reauth,
// 5.5) and mutates a credential, which is why it's modelled as POST rather
// than PATCH. No cache to invalidate: the response carries no profile body.
export function useChangePassword() {
  return useMutation({
    ...changePasswordMutation(),
  })
}
