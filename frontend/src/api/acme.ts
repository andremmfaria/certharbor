import { getAcmeCertificateCertificate, getAcmeCertificateKey } from '@kantharos-ai/certharbor-client'
import {
  createAcmeCertificateMutation,
  deleteAcmeCertificateMutation,
  getAcmeOptionsOptions,
  listAcmeCertificatesOptions,
  patchAcmeCertificateMutation,
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
