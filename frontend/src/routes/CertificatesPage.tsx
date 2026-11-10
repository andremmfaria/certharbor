import { useState } from 'react'
import { Link } from 'react-router'
import type { CertificateSummaryBody } from '@andremmfaria/certharbor-client'
import {
  downloadCaChain,
  downloadCaIntermediate,
  downloadCaRoot,
  downloadCertificateCrt,
  downloadCertificateKey,
  useCaInfo,
  useCertificates,
  useRenewCertificate,
  useRevokeCertificate,
} from '../api/certificates'
import { errorMessage } from '../api/errors'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'
import { useListParams } from '../hooks/useListParams'
import { useAuthenticatedUser } from '../auth/useAuthenticatedUser'
import { useConfig } from '../api/queries'
import { roleAllows } from '../navigation'

function keyTypeBadge(keyType: string) {
  return <span className="badge blue">{keyType}</span>
}

function statusBadge(status: string) {
  const kind = status === 'active' ? 'ok' : status === 'revoked' ? 'danger' : 'gray'
  return <span className={`badge ${kind}`}>{status}</span>
}

function CaPanel({ canDownload }: { canDownload: boolean }) {
  const ca = useCaInfo()
  const toast = useToast()

  const download = (label: string, fn: () => Promise<void>) =>
    fn().catch((e: unknown) => toast.error(errorMessage(e, `${label} download failed.`)))

  return (
    <div className="card" data-testid="ca-panel">
      <div className="card-header">
        <h3>Certificate Authority</h3>
      </div>
      <div className="card-body">
        {ca.isPending ? (
          <Skeleton rows={2} />
        ) : ca.isError ? (
          <ErrorState problem={ca.error} onRetry={() => void ca.refetch()} />
        ) : ca.data ? (
          <>
            <div className="info-row">
              <div className="info-key">Subject</div>
              <div className="info-val">{ca.data.subject}</div>
            </div>
            <div className="info-row">
              <div className="info-key">SHA-256</div>
              <div className="info-val">
                <code>{ca.data.fingerprintSha256}</code>
              </div>
            </div>
            <div className="info-row">
              <div className="info-key">Reachable</div>
              <div className="info-val">
                <span className={`badge ${ca.data.reachable ? 'ok' : 'danger'}`}>
                  {ca.data.reachable ? 'yes' : 'no'}
                </span>
              </div>
            </div>
            {canDownload ? (
              <div className="page-actions">
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  onClick={() => download('Root CA', downloadCaRoot)}
                >
                  Download Root
                </button>
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  onClick={() => download('Intermediate CA', downloadCaIntermediate)}
                >
                  Download Intermediate
                </button>
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  onClick={() => download('CA chain', downloadCaChain)}
                >
                  Download Chain
                </button>
              </div>
            ) : null}
          </>
        ) : null}
      </div>
    </div>
  )
}

function CertificateRow({
  cert,
  canManage,
  canRevoke,
}: {
  cert: CertificateSummaryBody
  canManage: boolean
  canRevoke: boolean
}) {
  const toast = useToast()
  const renew = useRenewCertificate()
  const revoke = useRevokeCertificate()

  const onDownload = (label: string, fn: () => Promise<void>) =>
    fn().catch((e: unknown) => toast.error(errorMessage(e, `${label} download failed.`)))

  const onRenew = () =>
    renew.mutate(
      { path: { id: cert.id } },
      {
        onSuccess: () => toast.success(`${cert.name} renewed.`),
        onError: (e) => toast.error(errorMessage(e, 'Renew failed.')),
      },
    )

  const onRevoke = () => {
    if (!window.confirm(`Revoke ${cert.name}? This cannot be undone.`)) return
    revoke.mutate(
      { path: { id: cert.id } },
      {
        onSuccess: () => toast.success(`${cert.name} revoked.`),
        onError: (e) => toast.error(errorMessage(e, 'Revoke failed.')),
      },
    )
  }

  const hasKey = cert.keyType !== undefined && cert.keyType !== ''

  return (
    <tr>
      <td data-label="Name" className="cert-name-cell">
        <Link to={`/certificates/${cert.id}`}>{cert.name}</Link>
      </td>
      <td data-label="Domain" className="cert-domain-cell">
        <code>{cert.domain}</code>
      </td>
      <td data-label="Issued">{cert.issuedAt ? new Date(cert.issuedAt).toLocaleDateString() : '—'}</td>
      <td data-label="Expires">{cert.expiresAt ? new Date(cert.expiresAt).toLocaleDateString() : '—'}</td>
      <td data-label="Status">{statusBadge(cert.status)}</td>
      <td data-label="Type">{keyTypeBadge(cert.keyType)}</td>
      <td data-label="Serial" className="cert-serial-cell">
        <code>{cert.serial}</code>
      </td>
      <td data-label="Actions" className="cert-actions-cell">
        <div className="cert-actions">
          <Link className="btn btn-ghost btn-sm" to={`/certificates/${cert.id}`}>
            Details
          </Link>
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
              onClick={() => onDownload('Certificate', () => downloadCertificateCrt(cert.id, cert.name))}
            >
              CRT
            </button>
          ) : null}
          {canManage && hasKey ? (
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              onClick={() => onDownload('Key', () => downloadCertificateKey(cert.id, cert.name))}
            >
              KEY
            </button>
          ) : null}
          {canRevoke && cert.status === 'active' ? (
            <button
              type="button"
              className="btn btn-danger btn-sm"
              onClick={onRevoke}
              disabled={revoke.isPending}
            >
              Revoke
            </button>
          ) : null}
        </div>
      </td>
    </tr>
  )
}

export function CertificatesPage() {
  const user = useAuthenticatedUser()
  const config = useConfig(true)
  const { params, qInput, setQuery, setPage } = useListParams()
  const [status, setStatus] = useState('')
  const certs = useCertificates(params, status || undefined)

  const levels = config.data?.roleLevels ?? {}
  const canManage = roleAllows(user.role, 'manager', levels)
  const canRevoke = roleAllows(user.role, 'admin', levels)

  return (
    <section data-testid="page-certificates">
      <div className="page-header">
        <div>
          <div className="breadcrumb">certificates</div>
          <h1>Certificates</h1>
        </div>
        {canManage ? (
          <div className="page-actions">
            <Link className="btn btn-ghost" to="/import">
              Import
            </Link>
            <Link className="btn" to="/issue">
              Issue Certificate
            </Link>
          </div>
        ) : null}
      </div>

      <CaPanel canDownload={roleAllows(user.role, 'admin', levels)} />

      <div className="card cert-filter-card">
        <div className="card-body cert-filter-grid">
          <div className="form-group">
            <label htmlFor="cert-search">Search</label>
            <input
              id="cert-search"
              type="text"
              placeholder="Name or domain"
              value={qInput}
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
          <div className="form-group">
            <label htmlFor="cert-status">Status</label>
            <select id="cert-status" value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="">All</option>
              <option value="active">Active</option>
              <option value="revoked">Revoked</option>
              <option value="expired">Expired</option>
            </select>
          </div>
        </div>
      </div>

      {certs.isPending ? (
        <Skeleton rows={5} label="Loading certificates" />
      ) : certs.isError ? (
        <ErrorState problem={certs.error} onRetry={() => void certs.refetch()} />
      ) : !certs.data?.items || certs.data.items.length === 0 ? (
        <EmptyState title="No certificates">No certificates match the current filters.</EmptyState>
      ) : (
        <div className="card">
          <div className="card-table-wrap tbl-wrap">
            <table className="tbl cert-table">
              <thead>
                <tr>
                  <th className="cert-col-name">Name</th>
                  <th className="cert-col-domain">Domain</th>
                  <th className="cert-col-date">Issued</th>
                  <th className="cert-col-date">Expires</th>
                  <th className="cert-col-status">Status</th>
                  <th className="cert-col-type">Key Type</th>
                  <th className="cert-col-serial">Serial</th>
                  <th className="cert-col-actions">Actions</th>
                </tr>
              </thead>
              <tbody>
                {certs.data.items.map((cert) => (
                  <CertificateRow key={cert.id} cert={cert} canManage={canManage} canRevoke={canRevoke} />
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
