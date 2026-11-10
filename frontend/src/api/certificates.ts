import {
  getCaChain,
  getCaIntermediate,
  getCaRoot,
  getCertificateCertificate,
  getCertificateKey,
} from '@certharbor/client'
import {
  createCertificateMutation,
  getCaOptions,
  getCertificateOptions as getCertificateByIdOptions,
  getCertificateOptionsOptions,
  importCertificateUploadMutation,
  importCertificatesByPathsMutation,
  listCertificateImportCandidatesOptions,
  listCertificatesOptions,
  renewCertificateMutation,
  revokeCertificateMutation,
} from '@certharbor/client/react-query'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ListParams } from '../hooks/useListParams'
import { downloadBlob } from './downloadBlob'
import { invalidate } from './invalidation'

// List + filter bar state maps straight onto the operation's own query params,
// so there is nothing to translate between the URL and the request.
export function useCertificates(params: Pick<ListParams, 'page' | 'pageSize' | 'q'>, status?: string) {
  return useQuery(
    listCertificatesOptions({
      query: { page: params.page, pageSize: params.pageSize, q: params.q || undefined, status },
    }),
  )
}

export function useCertificate(id: number) {
  return useQuery(getCertificateByIdOptions({ path: { id } }))
}

export function useCertificateOptions() {
  return useQuery(getCertificateOptionsOptions())
}

export function useCaInfo() {
  return useQuery(getCaOptions())
}

export function useCreateCertificate() {
  const queryClient = useQueryClient()
  return useMutation({
    ...createCertificateMutation(),
    onSuccess: () => invalidate(queryClient, 'certificates'),
  })
}

export function useRenewCertificate() {
  const queryClient = useQueryClient()
  return useMutation({
    ...renewCertificateMutation(),
    onSuccess: () => invalidate(queryClient, 'certificates'),
  })
}

export function useRevokeCertificate() {
  const queryClient = useQueryClient()
  return useMutation({
    ...revokeCertificateMutation(),
    onSuccess: () => invalidate(queryClient, 'certificates'),
  })
}

export function useImportCandidates() {
  return useQuery(listCertificateImportCandidatesOptions())
}

export function useImportCertificateUpload() {
  const queryClient = useQueryClient()
  return useMutation({
    ...importCertificateUploadMutation(),
    onSuccess: () => invalidate(queryClient, 'certificates'),
  })
}

export function useImportCertificatesByPaths() {
  const queryClient = useQueryClient()
  return useMutation({
    ...importCertificatesByPathsMutation(),
    onSuccess: () => invalidate(queryClient, 'certificates'),
  })
}

// Every binary download goes through the same three steps: fetch the blob
// with the generated client (so auth/CSRF/baseUrl setup is never duplicated),
// name the file from the resource it belongs to, hand off to downloadBlob.
async function fetchAndDownload(
  fetcher: () => Promise<{ data?: Blob | File; error?: unknown }>,
  filename: string,
): Promise<void> {
  const { data, error } = await fetcher()
  if (error || !data) throw error ?? new Error('Download failed')
  downloadBlob(data instanceof Blob ? data : new Blob([data]), filename)
}

export function downloadCertificateCrt(id: number, name: string): Promise<void> {
  return fetchAndDownload(() => getCertificateCertificate({ path: { id } }), `${name}.crt`)
}

export function downloadCertificateKey(id: number, name: string): Promise<void> {
  return fetchAndDownload(() => getCertificateKey({ path: { id } }), `${name}.key`)
}

export function downloadCaRoot(): Promise<void> {
  return fetchAndDownload(() => getCaRoot(), 'root_ca.crt')
}

export function downloadCaIntermediate(): Promise<void> {
  return fetchAndDownload(() => getCaIntermediate(), 'intermediate_ca.crt')
}

export function downloadCaChain(): Promise<void> {
  return fetchAndDownload(() => getCaChain(), 'ca_chain.pem')
}
