import { useDashboard } from '../api/dashboard'
import { useAuthenticatedUser } from '../auth/useAuthenticatedUser'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'

// GET /api/v1/dashboard is viewer-role and composes only certificate counts
// (from listCertificates) and CA status (from getCA) — never audit or user
// data (D10 rule 2). There is no ACME count yet: Phase 7 has not landed
// GET /api/v1/acme/* (D10 rule 1), so the backend aggregate carries no field
// for it and this page shows none either.
export function DashboardPage() {
  const user = useAuthenticatedUser()
  const dashboard = useDashboard()

  return (
    <section data-testid="page-dashboard">
      <div className="page-header">
        <div>
          <div className="breadcrumb">dashboard</div>
          <h1>Welcome, {user.username}</h1>
        </div>
      </div>

      {dashboard.isPending ? (
        <Skeleton rows={3} label="Loading dashboard" />
      ) : dashboard.isError ? (
        <ErrorState problem={dashboard.error} onRetry={() => void dashboard.refetch()} />
      ) : (
        <div className="dashboard-grid">
          <div className="card" data-testid="dashboard-certificates">
            <div className="card-header">Certificates</div>
            <div className="card-body">
              <div className="stat-row">
                <span className="stat-value">{dashboard.data.certificates.total}</span>
                <span className="stat-label">total</span>
              </div>
              <div className="stat-row">
                <span className="stat-value">{dashboard.data.certificates.expiringSoon}</span>
                <span className="stat-label">expiring soon</span>
              </div>
              <div className="page-actions">
                <span className="badge ok">{dashboard.data.certificates.byStatus.ok ?? 0} ok</span>
                <span className="badge warn">
                  {dashboard.data.certificates.byStatus.warning ?? 0} warning
                </span>
                <span className="badge danger">
                  {dashboard.data.certificates.byStatus.expired ?? 0} expired
                </span>
              </div>
            </div>
          </div>

          <div className="card" data-testid="dashboard-ca">
            <div className="card-header">Certificate Authority</div>
            <div className="card-body">
              <div className="stat-row">
                <span className={`badge ${dashboard.data.ca.reachable ? 'ok' : 'danger'}`}>
                  {dashboard.data.ca.reachable ? 'reachable' : 'unreachable'}
                </span>
              </div>
              <div className="text-muted">{dashboard.data.ca.subject}</div>
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
