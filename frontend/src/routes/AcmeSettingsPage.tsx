import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useAcmeSettings, usePatchAcmeSettings } from '../api/acme'
import { errorMessage } from '../api/errors'
import { problemToFieldErrors } from '../api/problemToFieldErrors'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'

interface AcmeSettingsForm {
  email: string
  provider: 'http01' | 'cloudflare' | 'route53' | 'manual'
  cfZoneId: string
  r53KeyId: string
  r53Region: string
  // writeOnly on the server (5.8): never pre-filled from GET, never echoed
  // back by PATCH's response. Blank means "leave the stored value unchanged".
  cfToken: string
  r53SecretKey: string
}

export function AcmeSettingsPage() {
  const settings = useAcmeSettings()
  const patch = usePatchAcmeSettings()
  const toast = useToast()

  const {
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors },
  } = useForm<AcmeSettingsForm>()

  useEffect(() => {
    if (!settings.data) return
    reset({
      email: settings.data.email ?? '',
      provider: (settings.data.provider ?? 'http01') as AcmeSettingsForm['provider'],
      cfZoneId: settings.data.cfZoneId ?? '',
      r53KeyId: settings.data.r53KeyId ?? '',
      r53Region: settings.data.r53Region ?? '',
      cfToken: '',
      r53SecretKey: '',
    })
  }, [settings.data, reset])

  const onSubmit = handleSubmit((values) => {
    patch.mutate(
      {
        body: {
          email: values.email,
          provider: values.provider,
          cfZoneId: values.cfZoneId,
          r53KeyId: values.r53KeyId,
          r53Region: values.r53Region,
          // Omit when blank: a merge-style PATCH leaves the stored
          // provider secret unchanged (5.8), never clears it.
          ...(values.cfToken ? { cfToken: values.cfToken } : {}),
          ...(values.r53SecretKey ? { r53SecretKey: values.r53SecretKey } : {}),
        },
      },
      {
        onSuccess: () => {
          toast.success('ACME settings saved.')
          reset((current) => ({ ...current, cfToken: '', r53SecretKey: '' }))
        },
        onError: (e) => {
          toast.error(errorMessage(e, 'Save failed.'))
          for (const [field, message] of Object.entries(problemToFieldErrors(e as never))) {
            setError(field as keyof AcmeSettingsForm, { message })
          }
        },
      },
    )
  })

  const [showSecrets, setShowSecrets] = useState(false)

  return (
    <section data-testid="page-acme-settings">
      <div className="page-header">
        <div>
          <div className="breadcrumb">acme &middot; settings</div>
          <h1>ACME settings</h1>
        </div>
      </div>

      {settings.isPending ? (
        <Skeleton rows={6} label="Loading ACME settings" />
      ) : settings.isError ? (
        <ErrorState problem={settings.error} onRetry={() => void settings.refetch()} />
      ) : (
        <form onSubmit={onSubmit} noValidate className="form-grid" data-testid="acme-settings-form">
          <div className="card">
            <div className="card-header">
              <h3>Provider</h3>
            </div>
            <div className="card-body form-grid">
              <div className="form-group">
                <label htmlFor="acme-email">Account email</label>
                <input id="acme-email" type="text" {...register('email')} />
                {errors.email ? <div className="field-error">{errors.email.message}</div> : null}
              </div>
              <div className="form-group">
                <label htmlFor="acme-provider">Challenge provider</label>
                <select id="acme-provider" {...register('provider')}>
                  <option value="http01">HTTP-01</option>
                  <option value="cloudflare">Cloudflare DNS-01</option>
                  <option value="route53">AWS Route53 DNS-01</option>
                  <option value="manual">Manual</option>
                </select>
              </div>
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="acme-cf-zone">Cloudflare zone ID</label>
                  <input id="acme-cf-zone" type="text" {...register('cfZoneId')} />
                </div>
                <div className="form-group">
                  <label htmlFor="acme-cf-token">Cloudflare API token</label>
                  <input
                    id="acme-cf-token"
                    type={showSecrets ? 'text' : 'password'}
                    autoComplete="new-password"
                    placeholder="Leave blank to keep the current token"
                    {...register('cfToken')}
                  />
                </div>
              </div>
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="acme-r53-key">Route53 access key ID</label>
                  <input id="acme-r53-key" type="text" {...register('r53KeyId')} />
                </div>
                <div className="form-group">
                  <label htmlFor="acme-r53-secret">Route53 secret key</label>
                  <input
                    id="acme-r53-secret"
                    type={showSecrets ? 'text' : 'password'}
                    autoComplete="new-password"
                    placeholder="Leave blank to keep the current secret"
                    {...register('r53SecretKey')}
                  />
                </div>
              </div>
              <div className="form-group">
                <label htmlFor="acme-r53-region">Route53 region</label>
                <input id="acme-r53-region" type="text" {...register('r53Region')} />
              </div>
              <label className="form-check">
                <input
                  type="checkbox"
                  checked={showSecrets}
                  onChange={(e) => setShowSecrets(e.target.checked)}
                />
                <span>Show secrets</span>
              </label>
            </div>
          </div>

          <div className="page-actions">
            <button className="btn" type="submit" disabled={patch.isPending}>
              {patch.isPending ? 'Saving…' : 'Save settings'}
            </button>
          </div>
        </form>
      )}
    </section>
  )
}
