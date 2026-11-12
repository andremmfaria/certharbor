import { useAdminAbout } from '../api/admin'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'

function checkBadge(status: string) {
  const cls = status === 'ok' ? 'ok' : status === 'warn' ? 'warn' : 'danger'
  return <span className={`badge ${cls}`}>{status}</span>
}

export function AboutPage() {
  const about = useAdminAbout()

  return (
    <section data-testid="page-admin-about">
      <div className="page-header">
        <div>
          <div className="breadcrumb">admin &middot; about</div>
          <h1>About</h1>
        </div>
      </div>

      {about.isPending ? (
        <Skeleton rows={5} label="Loading system information" />
      ) : about.isError ? (
        <ErrorState problem={about.error} onRetry={() => void about.refetch()} />
      ) : (
        <>
          <div className="card" data-testid="about-system">
            <div className="card-header">System</div>
            <div className="card-body">
              <div className="info-row">
                <span className="info-label">Version</span>
                <span>{about.data.system.version}</span>
              </div>
              <div className="info-row">
                <span className="info-label">Build date</span>
                <span>{about.data.system.buildDate}</span>
              </div>
              <div className="info-row">
                <span className="info-label">Git commit</span>
                <span>
                  <code>{about.data.system.gitCommit}</code>
                </span>
              </div>
              <div className="info-row">
                <span className="info-label">Started</span>
                <span>{new Date(about.data.system.startedAt).toLocaleString()}</span>
              </div>
              <div className="info-row">
                <span className="info-label">Uptime</span>
                <span>{about.data.system.uptime}</span>
              </div>
              <div className="info-row">
                <span className="info-label">CA URL</span>
                <span>{about.data.system.caUrl}</span>
              </div>
              <div className="info-row">
                <span className="info-label">Provisioner</span>
                <span>{about.data.system.provisioner}</span>
              </div>
              <div className="info-row">
                <span className="info-label">step-ca image</span>
                <span>{about.data.system.stepCaImage}</span>
              </div>
              <div className="info-row">
                <span className="info-label">Secure sessions</span>
                <span className={`badge ${about.data.system.sessionSecure ? 'ok' : 'warn'}`}>
                  {about.data.system.sessionSecure ? 'enabled' : 'disabled'}
                </span>
              </div>
            </div>
          </div>

          <div className="card">
            <div className="card-header">Preflight checks</div>
            <div className="card-body">
              <div className="page-actions">
                <span className="badge ok">{about.data.summary.ok} ok</span>
                <span className="badge warn">{about.data.summary.warnings} warnings</span>
                <span className="badge danger">{about.data.summary.errors} errors</span>
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
                    {(about.data.checks ?? []).map((c) => (
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
        </>
      )}
    </section>
  )
}
