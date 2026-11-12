import { useAdminIntegrity } from '../api/admin'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'

function checkBadge(status: string) {
  const cls = status === 'ok' ? 'ok' : status === 'warn' ? 'warn' : 'danger'
  return <span className={`badge ${cls}`}>{status}</span>
}

// Deliberately uncached (useAdminIntegrity), matching the backend's
// getAdminIntegrity: Phase 6 step 2 measured the cost rather than guessing
// and found nothing here worth a poll or a TTL (api/admin.go).
export function IntegrityPage() {
  const integrity = useAdminIntegrity()

  return (
    <section data-testid="page-admin-integrity">
      <div className="page-header">
        <div>
          <div className="breadcrumb">admin &middot; integrity</div>
          <h1>CA integrity</h1>
        </div>
      </div>

      {integrity.isPending ? (
        <Skeleton rows={5} label="Loading CA integrity checks" />
      ) : integrity.isError ? (
        <ErrorState problem={integrity.error} onRetry={() => void integrity.refetch()} />
      ) : !integrity.data?.checks || integrity.data.checks.length === 0 ? (
        <EmptyState title="No checks">No integrity checks are configured.</EmptyState>
      ) : (
        <div className="card">
          <div className="card-body">
            <div className="page-actions">
              <span className="badge ok">{integrity.data.summary.ok} ok</span>
              <span className="badge warn">{integrity.data.summary.warnings} warnings</span>
              <span className="badge danger">{integrity.data.summary.errors} errors</span>
            </div>
            <div className="card-table-wrap tbl-wrap">
              <table className="tbl">
                <thead>
                  <tr>
                    <th>Check</th>
                    <th>Status</th>
                    <th>Detail</th>
                  </tr>
                </thead>
                <tbody>
                  {integrity.data.checks.map((c) => (
                    <tr key={c.name}>
                      <td data-label="Check">{c.name}</td>
                      <td data-label="Status">{checkBadge(c.status)}</td>
                      <td data-label="Detail" className="text-muted">
                        {c.detail}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
