import { Link } from 'react-router'
import type { AcmeCertificateBody } from '@kantharos-ai/certharbor-client'
import {
  downloadAcmeCrt,
  downloadAcmeKey,
  useAcmeCertificates,
  useDeleteAcmeCertificate,
  usePatchAcmeCertificateAutoRenew,
  useRenewAcmeCertificate,
} from '../api/acme'
import { errorMessage } from '../api/errors'
import { useConfig } from '../api/queries'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'
import { useListParams } from '../hooks/useListParams'
import { useAuthenticatedUser } from '../auth/useAuthenticatedUser'
import { roleAllows } from '../navigation'

function statusBadge(status: string) {
  const kind = status === 'active' ? 'ok' : status === 'error' ? 'danger' : 'gray'
  return <span className={`badge ${kind}`}>{status}</span>
}

function AcmeCertificateRow({ cert, canManage }: { cert: AcmeCertificateBody; canManage: boolean }) {
  const toast = useToast()
  const renew = useRenewAcmeCertificate()
  const remove = useDeleteAcmeCertificate()
  const patch = usePatchAcmeCertificateAutoRenew()

  const onDownload = (label: string, fn: () => Promise<void>) =>
    fn().catch((e: unknown) => toast.error(errorMessage(e, `${label} download failed.`)))

  const onRenew = () =>
    renew.mutate(
      { path: { id: cert.id } },
      {
        onSuccess: () => toast.success(`${cert.domain} renewed.`),
        onError: (e) => toast.error(errorMessage(e, 'Renew failed.')),
      },
    )

  const onDelete = () => {
    if (!window.confirm(`Delete ${cert.domain}? This cannot be undone.`)) return
    remove.mutate(
      { path: { id: cert.id } },
      {
        onSuccess: () => toast.success(`${cert.domain} deleted.`),
        onError: (e) => toast.error(errorMessage(e, 'Delete failed.')),
      },
    )
  }

  const onToggleAutoRenew = () =>
    patch.mutate(
      { path: { id: cert.id }, body: { autoRenew: !cert.autoRenew } },
      {
        onError: (e) => toast.error(errorMessage(e, 'Updating auto-renew failed.')),
      },
    )

  return (
    <tr>
      <td data-label="Domain" className="cert-domain-cell">
        <code>{cert.domain}</code>
      </td>
      <td data-label="Email">{cert.email}</td>
      <td data-label="Provider">{cert.provider}</td>
      <td data-label="Issued">{cert.issuedAt ? new Date(cert.issuedAt).toLocaleDateString() : '—'}</td>
      <td data-label="Expires">{cert.expiresAt ? new Date(cert.expiresAt).toLocaleDateString() : '—'}</td>
      <td data-label="Status">{statusBadge(cert.status)}</td>
      <td data-label="Auto-renew">
        {canManage ? (
          <button
            type="button"
            className={`btn btn-ghost btn-sm${cert.autoRenew ? ' active' : ''}`}
            onClick={onToggleAutoRenew}
            disabled={patch.isPending}
            aria-pressed={cert.autoRenew}
          >
            {cert.autoRenew ? 'On' : 'Off'}
          </button>
        ) : (
          <span className={`badge ${cert.autoRenew ? 'ok' : 'gray'}`}>{cert.autoRenew ? 'On' : 'Off'}</span>
        )}
      </td>
      <td data-label="Actions" className="cert-actions-cell">
        <div className="cert-actions">
          {canManage ? (
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              onClick={onRenew}
              disabled={renew.isPending}
            >
              Renew
            </button>
          ) : null}
          {canManage ? (
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              onClick={() => onDownload('Certificate', () => downloadAcmeCrt(cert.id, cert.domain))}
            >
              CRT
            </button>
          ) : null}
          {canManage ? (
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              onClick={() => onDownload('Key', () => downloadAcmeKey(cert.id, cert.domain))}
            >
              KEY
            </button>
          ) : null}
          {canManage ? (
            <button
              type="button"
              className="btn btn-danger btn-sm"
              onClick={onDelete}
              disabled={remove.isPending}
            >
              Delete
            </button>
          ) : null}
        </div>
      </td>
    </tr>
  )
}

export function AcmeCertificatesPage() {
  const user = useAuthenticatedUser()
  const config = useConfig(true)
  const { params, setPage } = useListParams()
  const certs = useAcmeCertificates(params)

  const levels = config.data?.roleLevels ?? {}
  const canManage = roleAllows(user.role, 'manager', levels)

  return (
    <section data-testid="page-acme">
      <div className="page-header">
        <div>
          <div className="breadcrumb">acme</div>
          <h1>ACME Certificates</h1>
        </div>
        {canManage ? (
          <div className="page-actions">
            <Link className="btn" to="/acme/issue">
              Issue Certificate
            </Link>
          </div>
        ) : null}
      </div>

      {certs.isPending ? (
        <Skeleton rows={5} label="Loading ACME certificates" />
      ) : certs.isError ? (
        <ErrorState problem={certs.error} onRetry={() => void certs.refetch()} />
      ) : !certs.data?.items || certs.data.items.length === 0 ? (
        <EmptyState title="No ACME certificates">No ACME certificates match the current filters.</EmptyState>
      ) : (
        <div className="card">
          <div className="card-table-wrap tbl-wrap">
            <table className="tbl cert-table">
              <thead>
                <tr>
                  <th className="cert-col-domain">Domain</th>
                  <th>Email</th>
                  <th>Provider</th>
                  <th className="cert-col-date">Issued</th>
                  <th className="cert-col-date">Expires</th>
                  <th className="cert-col-status">Status</th>
                  <th>Auto-renew</th>
                  <th className="cert-col-actions">Actions</th>
                </tr>
              </thead>
              <tbody>
                {certs.data.items.map((cert) => (
                  <AcmeCertificateRow key={cert.id} cert={cert} canManage={canManage} />
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {certs.data && certs.data.totalPages && certs.data.totalPages > 1 ? (
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
            Page {certs.data.page} of {certs.data.totalPages}
          </span>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={params.page >= certs.data.totalPages}
            onClick={() => setPage(params.page + 1)}
          >
            Next
          </button>
        </div>
      ) : null}
    </section>
  )
}
