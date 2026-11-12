import { createBackupMutation, listBackupsOptions } from '@kantharos-ai/certharbor-client/react-query'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { invalidate } from './invalidation'

// GET /api/v1/admin/backups: the persisted bundle list, newest first.
export function useBackups() {
  return useQuery(listBackupsOptions())
}

// POST /api/v1/admin/backups creates and persists a bundle, returning
// {id, sizeBytes, downloadUrl}. The page then navigates to downloadUrl
// (navigateToBackupDownload below) rather than fetching a Blob here.
export function useCreateBackup() {
  const queryClient = useQueryClient()
  return useMutation({
    ...createBackupMutation(),
    onSuccess: () => invalidate(queryClient, 'admin'),
  })
}

// Mirrors the server's own constraint on backupBody.downloadUrl (5.7): a
// relative path under /api/v1/admin/backups/{id}/download. Checked again
// here so a malformed value never reaches window.location.assign — a path
// outside the proxied API prefix would fall through nginx's try_files and
// save the SPA shell as a corrupt backup.
const BACKUP_DOWNLOAD_URL_PATTERN = /^\/api\/v1\/admin\/backups\/[A-Za-z0-9_-]+\/download$/

// A hand-rolled download-by-navigation wrapper (5.7, R9): the archive is a
// full pg_dump and must never be buffered into JS heap as a Blob, so this
// is a real top-level, same-site browser navigation — which also keeps the
// Strict session cookie attached — rather than a generated-client call.
export function navigateToBackupDownload(downloadUrl: string): void {
  if (!BACKUP_DOWNLOAD_URL_PATTERN.test(downloadUrl)) {
    throw new Error('Unexpected backup download URL shape.')
  }
  window.location.assign(downloadUrl)
}
