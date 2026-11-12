import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useNavigate } from 'react-router'
import { z } from 'zod'
import { useAcmeOptions, useCreateAcmeCertificate } from '../api/acme'
import { errorMessage, isProblem } from '../api/errors'
import { problemToFieldErrors } from '../api/problemToFieldErrors'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'

const issueSchema = z.object({
  domain: z.string().trim().min(1, 'Domain is required'),
  email: z.string().trim().min(1, 'Email is required'),
  provider: z.string(),
  autoRenew: z.boolean(),
})
type IssueForm = z.infer<typeof issueSchema>

export function AcmeIssuePage() {
  const options = useAcmeOptions()
  const createAcmeCertificate = useCreateAcmeCertificate()
  const toast = useToast()
  const navigate = useNavigate()
  const [formError, setFormError] = useState<string | undefined>()

  const {
    register,
    handleSubmit,
    setError,
    setValue,
    watch,
    formState: { errors },
  } = useForm<IssueForm>({
    resolver: zodResolver(issueSchema),
    defaultValues: { domain: '', email: '', provider: '', autoRenew: true },
  })

  const selected = watch()

  // Seed the form from the server's defaults once they arrive, so the
  // picker opens on a valid, server-sanctioned provider/email, same as
  // IssuePage.tsx's template-seeding effect.
  useEffect(() => {
    if (!options.data) return
    if (!selected.provider && options.data.defaultProvider) setValue('provider', options.data.defaultProvider)
    if (!selected.email && options.data.defaultEmail) setValue('email', options.data.defaultEmail)
  }, [options.data, selected.email, selected.provider, setValue])

  const onSubmit = handleSubmit((values) => {
    setFormError(undefined)
    createAcmeCertificate.mutate(
      { body: values },
      {
        onSuccess: (cert) => {
          toast.success(`${cert.domain} issuance started.`)
          void navigate('/acme')
        },
        onError: (problem) => {
          const fields = isProblem(problem) ? problemToFieldErrors(problem) : {}
          const names = Object.keys(fields)
          if (names.length > 0) {
            for (const name of names) setError(name as keyof IssueForm, { message: fields[name] })
            return
          }
          setFormError(errorMessage(problem, 'Issuing the ACME certificate failed.'))
        },
      },
    )
  })

  return (
    <section data-testid="page-acme-issue">
      <div className="page-header">
        <div>
          <div className="breadcrumb">acme / issue</div>
          <h1>Issue a Let&apos;s Encrypt certificate</h1>
        </div>
      </div>

      {options.isPending ? (
        <Skeleton rows={6} label="Loading ACME options" />
      ) : options.isError ? (
        <ErrorState problem={options.error} onRetry={() => void options.refetch()} />
      ) : (
        <form onSubmit={onSubmit} noValidate className="form-row" style={{ gridTemplateColumns: '2fr 1fr' }}>
          <div>
            <div className="card">
              <div className="card-body form-grid">
                <div className="form-group">
                  <label htmlFor="domain">Domain</label>
                  <input id="domain" type="text" {...register('domain')} />
                  {errors.domain ? <div className="field-error">{errors.domain.message}</div> : null}
                </div>
                <div className="form-group">
                  <label htmlFor="email">Email</label>
                  <input id="email" type="email" {...register('email')} />
                  {errors.email ? <div className="field-error">{errors.email.message}</div> : null}
                </div>
              </div>
            </div>

            <div className="card">
              <div className="card-header">
                <h3>Provider</h3>
              </div>
              <div className="card-body chip-row">
                {(options.data?.providers ?? []).map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    className={`chip${selected.provider === p.id ? ' active' : ''}`}
                    onClick={() => setValue('provider', p.id)}
                  >
                    {p.label}
                  </button>
                ))}
              </div>
            </div>

            <div className="card">
              <div className="card-body">
                <label className="checkbox-row">
                  <input type="checkbox" {...register('autoRenew')} />
                  Auto-renew this certificate
                </label>
              </div>
            </div>

            <input type="hidden" {...register('provider')} />

            {formError ? (
              <div className="error" role="alert" data-testid="acme-issue-error">
                {formError}
              </div>
            ) : null}
            <div className="page-actions">
              <button className="btn" type="submit" disabled={createAcmeCertificate.isPending}>
                Issue Certificate
              </button>
            </div>
          </div>

          <div className="card">
            <div className="card-header">
              <h3>Preview</h3>
            </div>
            <div className="card-body">
              <div className="info-row">
                <div className="info-key">Domain</div>
                <div className="info-val">{selected.domain || '—'}</div>
              </div>
              <div className="info-row">
                <div className="info-key">Email</div>
                <div className="info-val">{selected.email || '—'}</div>
              </div>
              <div className="info-row">
                <div className="info-key">Provider</div>
                <div className="info-val">{selected.provider || '—'}</div>
              </div>
              <div className="info-row">
                <div className="info-key">Auto-renew</div>
                <div className="info-val">{selected.autoRenew ? 'Yes' : 'No'}</div>
              </div>
            </div>
          </div>
        </form>
      )}
    </section>
  )
}
