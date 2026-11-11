import { useAuditEvents } from '../api/security'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useListParams } from '../hooks/useListParams'

function outcomeBadge(outcome: string) {
  return <span className={`badge ${outcome === 'success' ? 'ok' : 'danger'}`}>{outcome}</span>
}

export function ActivityPage() {
  const { params, setPage } = useListParams()
  const events = useAuditEvents(params)

  return (
    <section data-testid="page-admin-activity">
      <div className="page-header">
        <div>
          <div className="breadcrumb">admin &middot; activity</div>
          <h1>CA activity</h1>
        </div>
      </div>

      {events.isPending ? (
        <Skeleton rows={5} label="Loading activity" />
      ) : events.isError ? (
        <ErrorState problem={events.error} onRetry={() => void events.refetch()} />
      ) : !events.data?.items || events.data.items.length === 0 ? (
        <EmptyState title="No events">No activity has been recorded yet.</EmptyState>
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
