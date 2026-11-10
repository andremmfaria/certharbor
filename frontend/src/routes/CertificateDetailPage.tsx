import { Link, useParams } from 'react-router'
import { downloadCertificateCrt, downloadCertificateKey, useCertificate } from '../api/certificates'
import { errorMessage } from '../api/errors'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'

function worstStatus(validations: { status: string }[] | null | undefined): 'ok' | 'warn' | 'err' {
  if (!validations || validations.length === 0) return 'ok'
  if (validations.some((v) => v.status === 'err')) return 'err'
  if (validations.some((v) => v.status === 'warn')) return 'warn'
  return 'ok'
}

function deploySnippets(domain: string, certPath: string, keyPath: string) {
  return {
    nginx: `server {\n    server_name ${domain};\n    ssl_certificate     ${certPath};\n    ssl_certificate_key ${keyPath};\n}`,
    apache: `<VirtualHost *:443>\n    ServerName ${domain}\n    SSLEngine on\n    SSLCertificateFile ${certPath}\n    SSLCertificateKeyFile ${keyPath}\n</VirtualHost>`,
    traefik: `tls:\n  certificates:\n    - certFile: ${certPath}\n      keyFile: ${keyPath}`,
  }
}

export function CertificateDetailPage() {
  const params = useParams<{ id: string }>()
  const id = Number(params.id)
  const certificate = useCertificate(id)
  const toast = useToast()

  const onDownload = (label: string, fn: () => Promise<void>) =>
    fn().catch((e: unknown) => toast.error(errorMessage(e, `${label} download failed.`)))

  return (
    <section data-testid="page-certificate-detail">
      <div className="page-header">
        <div>
          <Link to="/certificates" className="back-link" aria-label="Back to certificates">
            &larr;
          </Link>
          <div className="breadcrumb">certificate-detail</div>
          <h1>Certificate {params.id}</h1>
        </div>
        <div className="page-actions">
          <button
            type="button"
            className="btn btn-ghost"
            onClick={() => onDownload('Certificate', () => downloadCertificateCrt(id, `certificate-${id}`))}
          >
            Download CRT
          </button>
          <button
            type="button"
            className="btn btn-ghost"
            onClick={() => onDownload('Key', () => downloadCertificateKey(id, `certificate-${id}`))}
          >
            Download KEY
          </button>
        </div>
      </div>

      {certificate.isPending ? (
        <Skeleton rows={6} label="Loading certificate" />
      ) : certificate.isError ? (
        <ErrorState problem={certificate.error} onRetry={() => void certificate.refetch()} />
      ) : !certificate.data ? (
        <EmptyState title="Not found">This certificate could not be loaded.</EmptyState>
      ) : (
        (() => {
          const cert = certificate.data
          const domain = cert.dnsNames?.[0] ?? '—'
          const status = worstStatus(cert.validations)
          const snippets = deploySnippets(domain, `/certs/${id}/certificate.crt`, `/certs/${id}/key.pem`)
          return (
            <>
              <div className="home-summary">
                <div className="home-stat">
                  <div
                    className={`stat-value ${status === 'ok' ? 'ok' : status === 'warn' ? 'warn' : 'danger'}`}
                  >
                    {status}
                  </div>
                  <div className="stat-cap">Status</div>
                </div>
                <div className="home-stat">
                  <div className="stat-value">{cert.daysLeft ?? '—'}</div>
                  <div className="stat-cap">Days Remaining</div>
                </div>
                <div className="home-stat">
                  <div className="stat-value">{cert.signatureAlgorithm ?? '—'}</div>
                  <div className="stat-cap">Signature Algorithm</div>
                </div>
                <div className="home-stat">
                  <div className="stat-value">{domain}</div>
                  <div className="stat-cap">Domain</div>
                </div>
              </div>

              <div className="card">
                <div className="card-header">
                  <h3>Validations</h3>
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
                      {(cert.validations ?? []).map((v) => (
                        <tr key={v.name}>
                          <td>{v.name}</td>
                          <td>
                            <span
                              className={`badge ${v.status === 'ok' ? 'ok' : v.status === 'warn' ? 'warn' : 'danger'}`}
                            >
                              {v.status}
                            </span>
                          </td>
                          <td>{v.detail}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>

              <div className="card">
                <div className="card-header">
                  <h3>Certificate Detail</h3>
                </div>
                <div className="card-body">
                  <div className="info-row">
                    <div className="info-key">Subject</div>
                    <div className="info-val">{cert.subject}</div>
                  </div>
                  <div className="info-row">
                    <div className="info-key">Issuer</div>
                    <div className="info-val">{cert.issuer}</div>
                  </div>
                  <div className="info-row">
                    <div className="info-key">Serial (hex)</div>
                    <div className="info-val">
                      <code>{cert.serialHex}</code>
                    </div>
                  </div>
                  <div className="info-row">
                    <div className="info-key">SHA-256</div>
                    <div className="info-val">
                      <code>{cert.fingerprintSha256}</code>
                    </div>
                  </div>
                  <div className="info-row">
                    <div className="info-key">SHA-1</div>
                    <div className="info-val">
                      <code>{cert.fingerprintSha1}</code>
                    </div>
                  </div>
                  <div className="info-row">
                    <div className="info-key">Validity</div>
                    <div className="info-val">
                      {cert.notBefore ? new Date(cert.notBefore).toLocaleString() : '—'} &rarr;{' '}
                      {cert.notAfter ? new Date(cert.notAfter).toLocaleString() : '—'}
                    </div>
                  </div>
                  <div className="info-row">
                    <div className="info-key">SANs</div>
                    <div className="info-val">{(cert.dnsNames ?? []).join(', ') || '—'}</div>
                  </div>
                  <div className="info-row">
                    <div className="info-key">Key Usage</div>
                    <div className="info-val">{(cert.keyUsage ?? []).join(', ') || '—'}</div>
                  </div>
                  <div className="info-row">
                    <div className="info-key">Ext Key Usage</div>
                    <div className="info-val">{(cert.extKeyUsage ?? []).join(', ') || '—'}</div>
                  </div>
                </div>
              </div>

              <div className="card">
                <div className="card-header">
                  <h3>Deploy Snippets</h3>
                </div>
                <div className="card-body form-grid">
                  <div className="form-group">
                    <label htmlFor="snippet-nginx">nginx</label>
                    <textarea id="snippet-nginx" readOnly rows={5} value={snippets.nginx} />
                  </div>
                  <div className="form-group">
                    <label htmlFor="snippet-apache">Apache</label>
                    <textarea id="snippet-apache" readOnly rows={6} value={snippets.apache} />
                  </div>
                  <div className="form-group">
                    <label htmlFor="snippet-traefik">Traefik</label>
                    <textarea id="snippet-traefik" readOnly rows={4} value={snippets.traefik} />
                  </div>
                </div>
              </div>
            </>
          )
        })()
      )}
    </section>
  )
}
