import { useState } from 'react'
import { useAcmeEvents } from '../api/acme'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useListParams } from '../hooks/useListParams'

function actionBadge(action: string) {
  const kind =
    action === 'issue' ? 'ok' : action === 'renew' ? 'blue' : action === 'error' ? 'danger' : 'gray'
  return <span className={`badge ${kind}`}>{action}</span>
}

export function AcmeEventsPage() {
  const { params, setPage } = useListParams()
  const [domain, setDomain] = useState('')
  const events = useAcmeEvents(params, domain || undefined)

  return (
    <section data-testid="page-acme-logs">
      <div className="page-header">
        <div>
          <div className="breadcrumb">acme &middot; events</div>
          <h1>ACME events</h1>
        </div>
      </div>

      <div className="card cert-filter-card">
        <div className="card-body cert-filter-grid">
          <div className="form-group">
            <label htmlFor="acme-events-domain">Domain</label>
            <input
              id="acme-events-domain"
              type="text"
              placeholder="Exact domain"
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
            />
          </div>
        </div>
      </div>

      {events.isPending ? (
        <Skeleton rows={5} label="Loading ACME events" />
      ) : events.isError ? (
        <ErrorState problem={events.error} onRetry={() => void events.refetch()} />
      ) : !events.data?.items || events.data.items.length === 0 ? (
        <EmptyState title="No events">No ACME events match the current filters.</EmptyState>
      ) : (
        <div className="card">
          <div className="card-table-wrap tbl-wrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Action</th>
                  <th>Domain</th>
                  <th>Message</th>
                </tr>
              </thead>
              <tbody>
                {events.data.items.map((entry) => (
                  <tr key={entry.id}>
                    <td data-label="Time" className="td-muted-ts">
                      {new Date(entry.createdAt).toLocaleString()}
                    </td>
                    <td data-label="Action">{actionBadge(entry.action ?? '')}</td>
                    <td data-label="Domain">
                      <code>{entry.domain}</code>
                    </td>
                    <td data-label="Message" className="text-muted">
                      {entry.message}
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
