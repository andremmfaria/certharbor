import { useState } from 'react'
import { useSecurityEvents, useUnblockIP } from '../api/security'
import { errorMessage } from '../api/errors'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'
import { useListParams } from '../hooks/useListParams'

function outcomeBadge(outcome: string) {
  return <span className={`badge ${outcome === 'success' ? 'ok' : 'danger'}`}>{outcome}</span>
}

export function SecurityPage() {
  const { params, qInput, setQuery, setPage } = useListParams()
  const [outcome, setOutcome] = useState<'success' | 'failure' | undefined>(undefined)
  const events = useSecurityEvents(params, params.q, outcome)
  const unblock = useUnblockIP()
  const toast = useToast()

  const onUnblock = (ip: string) =>
    unblock.mutate(
      { path: { ip } },
      {
        onSuccess: () => toast.success(`${ip} unblocked.`),
        onError: (e) => toast.error(errorMessage(e, 'Unblock failed.')),
      },
    )

  return (
    <section data-testid="page-admin-security">
      <div className="page-header">
        <div>
          <div className="breadcrumb">admin &middot; security</div>
          <h1>Security log</h1>
        </div>
      </div>

      <div className="card cert-filter-card">
        <div className="card-body cert-filter-grid">
          <div className="form-group">
            <label htmlFor="security-search">Search</label>
            <input
              id="security-search"
              type="text"
              placeholder="Username or IP"
              value={qInput}
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
          <div className="form-group">
            <label htmlFor="security-outcome">Outcome</label>
            <select
              id="security-outcome"
              value={outcome ?? ''}
              onChange={(e) => setOutcome((e.target.value || undefined) as 'success' | 'failure' | undefined)}
            >
              <option value="">All</option>
              <option value="success">Success</option>
              <option value="failure">Failure</option>
            </select>
          </div>
        </div>
      </div>

      {events.isPending ? (
        <Skeleton rows={5} label="Loading security events" />
      ) : events.isError ? (
        <ErrorState problem={events.error} onRetry={() => void events.refetch()} />
      ) : !events.data?.items || events.data.items.length === 0 ? (
        <EmptyState title="No events">No security events match the current filters.</EmptyState>
      ) : (
        <div className="card">
          <div className="card-table-wrap tbl-wrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Outcome</th>
                  <th>Username</th>
                  <th>IP</th>
                  <th>Reason</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {events.data.items.map((entry) => (
                  <tr key={entry.id}>
                    <td data-label="Time" className="td-muted-ts">
                      {new Date(entry.createdAt).toLocaleString()}
                    </td>
                    <td data-label="Outcome">{outcomeBadge(entry.outcome)}</td>
                    <td data-label="Username">{entry.username}</td>
                    <td data-label="IP">
                      <code>{entry.ip}</code>
                    </td>
                    <td data-label="Reason" className="text-muted">
                      {entry.reason}
                    </td>
                    <td data-label="Actions">
                      {entry.outcome === 'failure' && entry.ip ? (
                        <button
                          type="button"
                          className="btn btn-ghost btn-sm"
                          disabled={unblock.isPending}
                          onClick={() => onUnblock(entry.ip!)}
                        >
                          Unblock IP
                        </button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {events.data && events.data.totalPages && events.data.totalPages > 1 ? (
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
            Page {events.data.page} of {events.data.totalPages}
          </span>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={params.page >= events.data.totalPages}
            onClick={() => setPage(params.page + 1)}
          >
            Next
          </button>
        </div>
      ) : null}
    </section>
  )
}
