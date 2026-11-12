import { getAcmeCertificateCertificate, getAcmeCertificateKey } from '@kantharos-ai/certharbor-client'
import {
  createAcmeCertificateMutation,
  deleteAcmeCertificateMutation,
  getAcmeOptionsOptions,
  getAcmeSettingsOptions,
  listAcmeCertificatesOptions,
  listAcmeEventsOptions,
  patchAcmeCertificateMutation,
  patchAcmeSettingsMutation,
  renewAcmeCertificateMutation,
} from '@kantharos-ai/certharbor-client/react-query'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ListParams } from '../hooks/useListParams'
import { downloadBlob } from './downloadBlob'
import { invalidate } from './invalidation'

// List + filter bar state maps straight onto the operation's own query
// params, the same convention useCertificates already uses.
export function useAcmeCertificates(params: Pick<ListParams, 'page' | 'pageSize'>) {
  return useQuery(listAcmeCertificatesOptions({ query: { page: params.page, pageSize: params.pageSize } }))
}

export function useAcmeOptions() {
  return useQuery(getAcmeOptionsOptions())
}

export function useCreateAcmeCertificate() {
  const queryClient = useQueryClient()
  return useMutation({
    ...createAcmeCertificateMutation(),
    onSuccess: () => invalidate(queryClient, 'acme'),
  })
}

export function useRenewAcmeCertificate() {
  const queryClient = useQueryClient()
  return useMutation({
    ...renewAcmeCertificateMutation(),
    onSuccess: () => invalidate(queryClient, 'acme'),
  })
}

export function useDeleteAcmeCertificate() {
  const queryClient = useQueryClient()
  return useMutation({
    ...deleteAcmeCertificateMutation(),
    onSuccess: () => invalidate(queryClient, 'acme'),
  })
}

// PATCH is the toggle mechanic (5.8): the only field it carries is autoRenew.
export function usePatchAcmeCertificateAutoRenew() {
  const queryClient = useQueryClient()
  return useMutation({
    ...patchAcmeCertificateMutation(),
    onSuccess: () => invalidate(queryClient, 'acme'),
  })
}

// Mirrors certificates.ts' fetchAndDownload: every binary download goes
// through the generated client so auth/CSRF/baseUrl setup is never
// duplicated, then names the file from the resource it belongs to.
async function fetchAndDownload(
  fetcher: () => Promise<{ data?: Blob | File; error?: unknown }>,
  filename: string,
): Promise<void> {
  const { data, error } = await fetcher()
  if (error || !data) throw error ?? new Error('Download failed')
  downloadBlob(data instanceof Blob ? data : new Blob([data]), filename)
}

export function downloadAcmeCrt(id: number, domain: string): Promise<void> {
  return fetchAndDownload(() => getAcmeCertificateCertificate({ path: { id } }), `${domain}.crt`)
}

export function downloadAcmeKey(id: number, domain: string): Promise<void> {
  return fetchAndDownload(() => getAcmeCertificateKey({ path: { id } }), `${domain}.key`)
}

// GET /api/v1/acme/settings. cfToken and r53SecretKey are writeOnly and
// never present in this response (5.8), so the form fields they back
// start blank on every load and a save that omits them leaves the stored
// value unchanged.
export function useAcmeSettings() {
  return useQuery(getAcmeSettingsOptions())
}

// PATCH /api/v1/acme/settings is merge-style: absent fields are
// unchanged, so the form only sends cfToken/r53SecretKey when the
// manager typed a new value.
export function usePatchAcmeSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    ...patchAcmeSettingsMutation(),
    onSuccess: () => invalidate(queryClient, 'acme'),
  })
}

// GET /api/v1/acme/events: paginated le_logs feed with an optional exact
// domain filter, same envelope and bounds as history/security events.
export function useAcmeEvents(params: Pick<ListParams, 'page' | 'pageSize'>, domain: string | undefined) {
  return useQuery(
    listAcmeEventsOptions({
      query: { page: params.page, pageSize: params.pageSize, domain: domain || undefined },
    }),
  )
}
