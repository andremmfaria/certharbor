import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import {
  useImportCandidates,
  useImportCertificateUpload,
  useImportCertificatesByPaths,
} from '../api/certificates'
import { errorMessage } from '../api/errors'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'

type Tab = 'upload' | 'candidates' | 'manual'

const uploadSchema = z.object({
  name: z.string().trim().min(1, 'Name is required'),
  domain: z.string().trim().min(1, 'Domain is required'),
})
type UploadForm = z.infer<typeof uploadSchema>

const manualSchema = z.object({
  name: z.string().trim().min(1, 'Name is required'),
  domain: z.string().trim().min(1, 'Domain is required'),
  certPath: z.string().trim().min(1, 'Certificate path is required'),
  keyPath: z.string().trim(),
})
type ManualForm = z.infer<typeof manualSchema>

function UploadTab() {
  const toast = useToast()
  const upload = useImportCertificateUpload()
  const [certFile, setCertFile] = useState<File | undefined>()
  const [keyFile, setKeyFile] = useState<File | undefined>()
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<UploadForm>({ resolver: zodResolver(uploadSchema) })

  const onSubmit = handleSubmit((values) => {
    if (!certFile || !keyFile) {
      toast.error('Select both a certificate and a key file.')
      return
    }
    upload.mutate(
      { body: { name: values.name, domain: values.domain, certFile, keyFile } },
      {
        onSuccess: (cert) => {
          toast.success(`${cert.name} imported.`)
          reset()
          setCertFile(undefined)
          setKeyFile(undefined)
        },
        onError: (e) => toast.error(errorMessage(e, 'Import failed.')),
      },
    )
  })

  return (
    <form onSubmit={onSubmit} noValidate className="form-grid" data-testid="import-upload-form">
      <div className="form-row">
        <div className="form-group">
          <label htmlFor="upload-name">Name</label>
          <input id="upload-name" type="text" {...register('name')} />
          {errors.name ? <div className="field-error">{errors.name.message}</div> : null}
        </div>
        <div className="form-group">
          <label htmlFor="upload-domain">Domain</label>
          <input id="upload-domain" type="text" {...register('domain')} />
          {errors.domain ? <div className="field-error">{errors.domain.message}</div> : null}
        </div>
      </div>
      <div className="form-row">
        <div className="form-group">
          <label htmlFor="upload-cert">Certificate file</label>
          <div className={`drop-zone${certFile ? ' has-file' : ''}`}>
            <input
              id="upload-cert"
              type="file"
              accept=".crt,.pem"
              onChange={(e) => setCertFile(e.target.files?.[0])}
            />
          </div>
          {certFile ? <div className="file-name">{certFile.name}</div> : null}
        </div>
        <div className="form-group">
          <label htmlFor="upload-key">Key file</label>
          <div className={`drop-zone${keyFile ? ' has-file' : ''}`}>
            <input
              id="upload-key"
              type="file"
              accept=".key,.pem"
              onChange={(e) => setKeyFile(e.target.files?.[0])}
            />
          </div>
          {keyFile ? <div className="file-name">{keyFile.name}</div> : null}
        </div>
      </div>
      <div className="page-actions">
        <button className="btn" type="submit" disabled={upload.isPending}>
          Import
        </button>
      </div>
    </form>
  )
}

function CandidatesTab() {
  const candidates = useImportCandidates()
  const importByPaths = useImportCertificatesByPaths()
  const toast = useToast()
  const [importingName, setImportingName] = useState<string | undefined>()

  const importOne = (name: string, certFile: string, keyFile?: string) => {
    setImportingName(name)
    importByPaths.mutate(
      { body: [{ name, domain: name, certPath: certFile, keyPath: keyFile }] },
      {
        onSuccess: (result) => {
          const first = result.results?.[0]
          if (first?.status === 'error') {
            toast.error(first.error ?? `${name} could not be imported.`)
          } else {
            toast.success(`${name} imported.`)
          }
        },
        onError: (e) => toast.error(errorMessage(e, 'Import failed.')),
        onSettled: () => setImportingName(undefined),
      },
    )
  }

  if (candidates.isPending) return <Skeleton rows={4} label="Loading candidates" />
  if (candidates.isError)
    return <ErrorState problem={candidates.error} onRetry={() => void candidates.refetch()} />
  if (!candidates.data || candidates.data.length === 0) {
    return <EmptyState title="No candidates">No unregistered certificates were found on disk.</EmptyState>
  }

  return (
    <div className="card-table-wrap tbl-wrap">
      <table className="tbl">
        <thead>
          <tr>
            <th>Name</th>
            <th>Certificate file</th>
            <th>Key file</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {candidates.data.map((candidate) => (
            <tr key={candidate.name}>
              <td>{candidate.name}</td>
              <td>
                <code>{candidate.certFile}</code>
              </td>
              <td>{candidate.keyFile ? <code>{candidate.keyFile}</code> : '—'}</td>
              <td>
                <button
                  type="button"
                  className="btn btn-sm"
                  disabled={importByPaths.isPending && importingName === candidate.name}
                  onClick={() => importOne(candidate.name, candidate.certFile, candidate.keyFile)}
                >
                  Import
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function ManualTab() {
  const toast = useToast()
  const importByPaths = useImportCertificatesByPaths()
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<ManualForm>({ resolver: zodResolver(manualSchema) })

  const onSubmit = handleSubmit((values) => {
    importByPaths.mutate(
      {
        body: [
          {
            name: values.name,
            domain: values.domain,
            certPath: values.certPath,
            keyPath: values.keyPath || undefined,
          },
        ],
      },
      {
        onSuccess: (result) => {
          const first = result.results?.[0]
          if (first?.status === 'error') {
            toast.error(first.error ?? 'Import failed.')
            return
          }
          toast.success(`${values.name} imported.`)
          reset()
        },
        onError: (e) => toast.error(errorMessage(e, 'Import failed.')),
      },
    )
  })

  return (
    <form onSubmit={onSubmit} noValidate className="form-grid" data-testid="import-manual-form">
      <div className="form-row">
        <div className="form-group">
          <label htmlFor="manual-name">Name</label>
          <input id="manual-name" type="text" {...register('name')} />
          {errors.name ? <div className="field-error">{errors.name.message}</div> : null}
        </div>
        <div className="form-group">
          <label htmlFor="manual-domain">Domain</label>
          <input id="manual-domain" type="text" {...register('domain')} />
          {errors.domain ? <div className="field-error">{errors.domain.message}</div> : null}
        </div>
      </div>
      <div className="form-group">
        <label htmlFor="manual-cert-path">Certificate path</label>
        <input id="manual-cert-path" type="text" {...register('certPath')} />
        {errors.certPath ? <div className="field-error">{errors.certPath.message}</div> : null}
      </div>
      <div className="form-group">
        <label htmlFor="manual-key-path">Key path</label>
        <input id="manual-key-path" type="text" {...register('keyPath')} />
      </div>
      <div className="page-actions">
        <button className="btn" type="submit" disabled={importByPaths.isPending}>
          Import
        </button>
      </div>
    </form>
  )
}

export function ImportPage() {
  const [tab, setTab] = useState<Tab>('upload')

  return (
    <section data-testid="page-import">
      <div className="page-header">
        <div>
          <div className="breadcrumb">import</div>
          <h1>Import certificates</h1>
        </div>
      </div>

      <div className="chip-row" style={{ marginBottom: '14px' }}>
        <button
          type="button"
          className={`chip${tab === 'upload' ? ' active' : ''}`}
          onClick={() => setTab('upload')}
        >
          Upload
        </button>
        <button
          type="button"
          className={`chip${tab === 'candidates' ? ' active' : ''}`}
          onClick={() => setTab('candidates')}
        >
          Candidates
        </button>
        <button
          type="button"
          className={`chip${tab === 'manual' ? ' active' : ''}`}
          onClick={() => setTab('manual')}
        >
          Manual
        </button>
      </div>

      <div className="card">
        <div className="card-body">
          {tab === 'upload' ? <UploadTab /> : tab === 'candidates' ? <CandidatesTab /> : <ManualTab />}
        </div>
      </div>
    </section>
  )
}
