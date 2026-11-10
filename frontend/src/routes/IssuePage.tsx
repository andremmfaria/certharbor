import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useNavigate } from 'react-router'
import { z } from 'zod'
import { useCertificateOptions, useCreateCertificate } from '../api/certificates'
import { errorMessage, isProblem } from '../api/errors'
import { problemToFieldErrors } from '../api/problemToFieldErrors'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'

const issueSchema = z.object({
  name: z.string().trim().min(1, 'Name is required'),
  domain: z.string().trim().min(1, 'Domain is required'),
  template: z.string(),
  keyType: z.string(),
  duration: z.string(),
})
type IssueForm = z.infer<typeof issueSchema>

export function IssuePage() {
  const options = useCertificateOptions()
  const createCertificate = useCreateCertificate()
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
    defaultValues: { name: '', domain: '', template: '', keyType: '', duration: '' },
  })

  const selected = watch()

  // Seed the form from the server's defaults once they arrive, so the picker
  // opens on a valid, server-sanctioned combination rather than nothing.
  useEffect(() => {
    if (!options.data || selected.template) return
    const defaultTemplate = options.data.templates?.find((t) => t.id === options.data?.defaultTemplate)
    if (defaultTemplate) {
      setValue('template', defaultTemplate.id)
      setValue('keyType', defaultTemplate.defaultKeyType)
      setValue('duration', defaultTemplate.defaultDuration)
    }
  }, [options.data, selected.template, setValue])

  const pickTemplate = (templateId: string, keyType: string, duration: string) => {
    setValue('template', templateId)
    setValue('keyType', keyType)
    setValue('duration', duration)
  }

  const onSubmit = handleSubmit((values) => {
    setFormError(undefined)
    createCertificate.mutate(
      { body: values },
      {
        onSuccess: (cert) => {
          toast.success(`${cert.name} issued.`)
          void navigate(`/certificates/${cert.id}`)
        },
        onError: (problem) => {
          const fields = isProblem(problem) ? problemToFieldErrors(problem) : {}
          const names = Object.keys(fields)
          if (names.length > 0) {
            for (const name of names) setError(name as keyof IssueForm, { message: fields[name] })
            return
          }
          setFormError(errorMessage(problem, 'Issuing the certificate failed.'))
        },
      },
    )
  })

  return (
    <section data-testid="page-issue">
      <div className="page-header">
        <div>
          <div className="breadcrumb">issue</div>
          <h1>Issue a certificate</h1>
        </div>
      </div>

      {options.isPending ? (
        <Skeleton rows={6} label="Loading issuance options" />
      ) : options.isError ? (
        <ErrorState problem={options.error} onRetry={() => void options.refetch()} />
      ) : (
        <form onSubmit={onSubmit} noValidate className="form-row" style={{ gridTemplateColumns: '2fr 1fr' }}>
          <div>
            <div className="card">
              <div className="card-body form-grid">
                <div className="form-group">
                  <label htmlFor="name">Name</label>
                  <input id="name" type="text" {...register('name')} />
                  {errors.name ? <div className="field-error">{errors.name.message}</div> : null}
                </div>
                <div className="form-group">
                  <label htmlFor="domain">Domain</label>
                  <input id="domain" type="text" {...register('domain')} />
                  {errors.domain ? <div className="field-error">{errors.domain.message}</div> : null}
                </div>
              </div>
            </div>

            <div className="card">
              <div className="card-header">
                <h3>Template</h3>
              </div>
              <div className="card-body template-grid">
                {(options.data?.templates ?? []).map((template) => (
                  <button
                    key={template.id}
                    type="button"
                    className={`template-card${selected.template === template.id ? ' active' : ''}`}
                    data-template={template.id}
                    data-key={template.defaultKeyType}
                    data-duration={template.defaultDuration}
                    onClick={() =>
                      pickTemplate(template.id, template.defaultKeyType, template.defaultDuration)
                    }
                  >
                    <span className="template-title">{template.label}</span>
                    <span className="template-sub">{template.description}</span>
                  </button>
                ))}
              </div>
            </div>

            <div className="card">
              <div className="card-header">
                <h3>Key Type</h3>
              </div>
              <div className="card-body key-grid">
                {(options.data?.keyTypes ?? []).map((kt) => (
                  <button
                    key={kt.id}
                    type="button"
                    className={`key-card${selected.keyType === kt.id ? ' active' : ''}`}
                    onClick={() => setValue('keyType', kt.id)}
                  >
                    <div className="key-title">{kt.label}</div>
                    {kt.note ? <div className="key-sub">{kt.note}</div> : null}
                  </button>
                ))}
              </div>
            </div>

            <div className="card">
              <div className="card-header">
                <h3>Duration</h3>
              </div>
              <div className="card-body chip-row">
                {(options.data?.durations ?? []).map((d) => (
                  <button
                    key={d.id}
                    type="button"
                    className={`chip${selected.duration === d.id ? ' active' : ''}`}
                    onClick={() => setValue('duration', d.id)}
                  >
                    {d.label}
                  </button>
                ))}
              </div>
            </div>

            <input type="hidden" {...register('template')} />
            <input type="hidden" {...register('keyType')} />
            <input type="hidden" {...register('duration')} />

            {formError ? (
              <div className="error" role="alert" data-testid="issue-error">
                {formError}
              </div>
            ) : null}
            <div className="page-actions">
              <button className="btn" type="submit" disabled={createCertificate.isPending}>
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
                <div className="info-key">Name</div>
                <div className="info-val">{selected.name || '—'}</div>
              </div>
              <div className="info-row">
                <div className="info-key">Domain</div>
                <div className="info-val">{selected.domain || '—'}</div>
              </div>
              <div className="info-row">
                <div className="info-key">Template</div>
                <div className="info-val">{selected.template || '—'}</div>
              </div>
              <div className="info-row">
                <div className="info-key">Key Type</div>
                <div className="info-val">{selected.keyType || '—'}</div>
              </div>
              <div className="info-row">
                <div className="info-key">Duration</div>
                <div className="info-val">{selected.duration || '—'}</div>
              </div>
            </div>
          </div>
        </form>
      )}
    </section>
  )
}
