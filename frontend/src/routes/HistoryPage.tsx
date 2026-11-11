import { useState } from 'react'
import { useHistory } from '../api/history'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useListParams } from '../hooks/useListParams'

const ACTIONS = ['issue', 'renew', 'revoke', 'import']

function actionBadge(action: string) {
  const kind =
    action === 'issue' ? 'ok' : action === 'renew' ? 'blue' : action === 'revoke' ? 'danger' : 'gray'
  return <span className={`badge ${kind}`}>{action}</span>
}

function toggleAction(current: string[], action: string): string[] {
  return current.includes(action) ? current.filter((a) => a !== action) : [...current, action]
}

export function HistoryPage() {
  const { params, setPage } = useListParams()
  const [actions, setActions] = useState<string[]>([])
  const [cert, setCert] = useState('')
  const history = useHistory(params, actions, cert || undefined)

  return (
    <section data-testid="page-history">
      <div className="page-header">
        <div>
          <div className="breadcrumb">ca &middot; history</div>
          <h1>Operation History</h1>
        </div>
      </div>

      <div className="card cert-filter-card">
        <div className="card-body cert-filter-grid">
          <div className="form-group">
            <label htmlFor="history-cert">Certificate</label>
            <input
              id="history-cert"
              type="text"
              placeholder="Exact certificate name"
              value={cert}
              onChange={(e) => setCert(e.target.value)}
            />
          </div>
          <div className="form-group">
            <label>Actions</label>
            <div className="page-actions" role="group" aria-label="Action filter">
              {ACTIONS.map((action) => (
                <button
                  key={action}
                  type="button"
                  className={`btn btn-sm ${actions.includes(action) ? '' : 'btn-ghost'}`}
                  aria-pressed={actions.includes(action)}
                  onClick={() => setActions((current) => toggleAction(current, action))}
                >
                  {action}
                </button>
              ))}
              {actions.length > 0 ? (
                <button type="button" className="btn btn-ghost btn-sm" onClick={() => setActions([])}>
                  Reset
                </button>
              ) : null}
            </div>
          </div>
        </div>
      </div>

      {history.isPending ? (
        <Skeleton rows={5} label="Loading history" />
      ) : history.isError ? (
        <ErrorState problem={history.error} onRetry={() => void history.refetch()} />
      ) : !history.data?.items || history.data.items.length === 0 ? (
        <EmptyState title="No records">No history entries match the current filters.</EmptyState>
      ) : (
        <div className="card">
          <div className="card-table-wrap tbl-wrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Action</th>
                  <th>Certificate</th>
                  <th>Domain</th>
                  <th>Details</th>
                  <th>User</th>
                </tr>
              </thead>
              <tbody>
                {history.data.items.map((entry) => (
                  <tr key={entry.id}>
                    <td data-label="Time" className="td-muted-ts">
                      {new Date(entry.createdAt).toLocaleString()}
                    </td>
                    <td data-label="Action">{actionBadge(entry.action)}</td>
                    <td data-label="Certificate">
                      <strong>{entry.certName}</strong>
                    </td>
                    <td data-label="Domain">
                      <code>{entry.domain}</code>
                    </td>
                    <td data-label="Details" className="text-muted">
                      {entry.details}
                    </td>
                    <td data-label="User">{entry.username}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {history.data && history.data.totalPages && history.data.totalPages > 1 ? (
        <div className="page-actions">
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={params.page <= 1}
            onClick={() => setPage(params.page - 1)}
          >
            Previous
          </button>
          <span className="breadcrumb">
            Page {history.data.page} of {history.data.totalPages}
          </span>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={params.page >= history.data.totalPages}
            onClick={() => setPage(params.page + 1)}
          >
            Next
          </button>
        </div>
      ) : null}
    </section>
  )
}
