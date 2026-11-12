import { navigateToBackupDownload, useBackups, useCreateBackup } from '../api/backups'
import { errorMessage } from '../api/errors'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(1)} ${units[unit]}`
}

export function BackupsPage() {
  const backups = useBackups()
  const createBackup = useCreateBackup()
  const toast = useToast()

  const onCreate = () =>
    createBackup.mutate(
      {},
      {
        onSuccess: (backup) => {
          toast.success('Backup created. Download starting…')
          navigateToBackupDownload(backup.downloadUrl)
        },
        onError: (e) => toast.error(errorMessage(e, 'Backup creation failed.')),
      },
    )

  return (
    <section data-testid="page-admin-backup">
      <div className="page-header">
        <div>
          <div className="breadcrumb">admin &middot; backup</div>
          <h1>Backup</h1>
        </div>
        <div className="page-actions">
          <button type="button" className="btn" disabled={createBackup.isPending} onClick={onCreate}>
            {createBackup.isPending ? 'Creating…' : 'Create backup'}
          </button>
        </div>
      </div>

      {backups.isPending ? (
        <Skeleton rows={4} label="Loading backups" />
      ) : backups.isError ? (
        <ErrorState problem={backups.error} onRetry={() => void backups.refetch()} />
      ) : !backups.data || backups.data.length === 0 ? (
        <EmptyState title="No backups yet">Create a backup to download the first bundle.</EmptyState>
      ) : (
        <div className="card">
          <div className="card-table-wrap tbl-wrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Created</th>
                  <th>Id</th>
                  <th>Size</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {backups.data.map((backup) => (
                  <tr key={backup.id}>
                    <td data-label="Created" className="td-muted-ts">
                      {new Date(backup.createdAt).toLocaleString()}
                    </td>
                    <td data-label="Id">
                      <code>{backup.id}</code>
                    </td>
                    <td data-label="Size">{formatBytes(backup.sizeBytes)}</td>
                    <td data-label="Actions">
                      <a className="btn btn-ghost btn-sm" href={backup.downloadUrl}>
                        Download
                      </a>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </section>
  )
}
