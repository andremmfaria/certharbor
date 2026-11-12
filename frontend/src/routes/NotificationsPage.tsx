import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { errorMessage } from '../api/errors'
import { useNotificationSettings, usePatchNotifications, useTestNotifications } from '../api/notifications'
import { problemToFieldErrors } from '../api/problemToFieldErrors'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'

interface NotificationsForm {
  webhookEnabled: boolean
  webhookUrl: string
  notifyExpiry: boolean
  expiryDays: number
  notifyFailures: boolean
  notifyAuthBurst: boolean
  smtpEnabled: boolean
  smtpHost: string
  smtpPort: number
  smtpSecurity: 'none' | 'starttls' | 'tls'
  smtpUsername: string
  smtpFrom: string
  // writeOnly on the server (5.8): never pre-filled from GET, never echoed
  // back by PATCH's response. Blank means "leave the stored value unchanged".
  smtpPassword: string
}

export function NotificationsPage() {
  const settings = useNotificationSettings()
  const patch = usePatchNotifications()
  const test = useTestNotifications()
  const toast = useToast()

  const {
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors },
  } = useForm<NotificationsForm>()

  useEffect(() => {
    if (!settings.data) return
    reset({
      webhookEnabled: settings.data.webhookEnabled,
      webhookUrl: settings.data.webhookUrl ?? '',
      notifyExpiry: settings.data.notifyExpiry,
      expiryDays: settings.data.expiryDays,
      notifyFailures: settings.data.notifyFailures,
      notifyAuthBurst: settings.data.notifyAuthBurst,
      smtpEnabled: settings.data.smtpEnabled,
      smtpHost: settings.data.smtpHost ?? '',
      smtpPort: settings.data.smtpPort,
      smtpSecurity: (settings.data.smtpSecurity ?? 'starttls') as NotificationsForm['smtpSecurity'],
      smtpUsername: settings.data.smtpUsername ?? '',
      smtpFrom: settings.data.smtpFrom ?? '',
      smtpPassword: '',
    })
  }, [settings.data, reset])

  const onSubmit = handleSubmit((values) => {
    patch.mutate(
      {
        body: {
          webhookEnabled: values.webhookEnabled,
          webhookUrl: values.webhookUrl,
          notifyExpiry: values.notifyExpiry,
          expiryDays: Number(values.expiryDays),
          notifyFailures: values.notifyFailures,
          notifyAuthBurst: values.notifyAuthBurst,
          smtpEnabled: values.smtpEnabled,
          smtpHost: values.smtpHost,
          smtpPort: Number(values.smtpPort),
          smtpSecurity: values.smtpSecurity,
          smtpUsername: values.smtpUsername,
          smtpFrom: values.smtpFrom,
          // Omit when blank: a merge-style PATCH leaves the stored
          // password unchanged (5.8), never clears it.
          ...(values.smtpPassword ? { smtpPassword: values.smtpPassword } : {}),
        },
      },
      {
        onSuccess: () => {
          toast.success('Notification settings saved.')
          reset((current) => ({ ...current, smtpPassword: '' }))
        },
        onError: (e) => {
          toast.error(errorMessage(e, 'Save failed.'))
          for (const [field, message] of Object.entries(problemToFieldErrors(e as never))) {
            setError(field as keyof NotificationsForm, { message })
          }
        },
      },
    )
  })

  const onTest = () =>
    test.mutate(
      {},
      {
        onSuccess: () => toast.success('Test notification sent.'),
        onError: (e) => toast.error(errorMessage(e, 'Webhook test failed.')),
      },
    )

  const [showPassword, setShowPassword] = useState(false)

  return (
    <section data-testid="page-admin-notifications">
      <div className="page-header">
        <div>
          <div className="breadcrumb">admin &middot; notifications</div>
          <h1>Notifications</h1>
        </div>
      </div>

      {settings.isPending ? (
        <Skeleton rows={6} label="Loading notification settings" />
      ) : settings.isError ? (
        <ErrorState problem={settings.error} onRetry={() => void settings.refetch()} />
      ) : (
        <form onSubmit={onSubmit} noValidate className="form-grid" data-testid="notifications-form">
          <div className="card">
            <div className="card-header">
              <h3>Webhook</h3>
            </div>
            <div className="card-body form-grid">
              <label className="form-check">
                <input type="checkbox" {...register('webhookEnabled')} />
                <span>Enabled</span>
              </label>
              <div className="form-group">
                <label htmlFor="webhook-url">Webhook URL</label>
                <input id="webhook-url" type="text" {...register('webhookUrl')} />
                {errors.webhookUrl ? <div className="field-error">{errors.webhookUrl.message}</div> : null}
              </div>
              <label className="form-check">
                <input type="checkbox" {...register('notifyExpiry')} />
                <span>Notify on certificate expiry</span>
              </label>
              <div className="form-group">
                <label htmlFor="expiry-days">Expiry warning (days)</label>
                <input id="expiry-days" type="number" min={1} max={365} {...register('expiryDays')} />
              </div>
              <label className="form-check">
                <input type="checkbox" {...register('notifyFailures')} />
                <span>Notify on certificate operation failures</span>
              </label>
              <label className="form-check">
                <input type="checkbox" {...register('notifyAuthBurst')} />
                <span>Notify on authentication bursts</span>
              </label>
              <div className="page-actions">
                <button type="button" className="btn btn-ghost" disabled={test.isPending} onClick={onTest}>
                  {test.isPending ? 'Sending…' : 'Send test notification'}
                </button>
              </div>
            </div>
          </div>

          <div className="card">
            <div className="card-header">
              <h3>SMTP</h3>
            </div>
            <div className="card-body form-grid">
              <label className="form-check">
                <input type="checkbox" {...register('smtpEnabled')} />
                <span>Enabled</span>
              </label>
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="smtp-host">Host</label>
                  <input id="smtp-host" type="text" {...register('smtpHost')} />
                  {errors.smtpHost ? <div className="field-error">{errors.smtpHost.message}</div> : null}
                </div>
                <div className="form-group">
                  <label htmlFor="smtp-port">Port</label>
                  <input id="smtp-port" type="number" min={1} max={65535} {...register('smtpPort')} />
                </div>
              </div>
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="smtp-security">Security</label>
                  <select id="smtp-security" {...register('smtpSecurity')}>
                    <option value="none">None</option>
                    <option value="starttls">STARTTLS</option>
                    <option value="tls">TLS</option>
                  </select>
                </div>
                <div className="form-group">
                  <label htmlFor="smtp-username">Username</label>
                  <input id="smtp-username" type="text" {...register('smtpUsername')} />
                </div>
              </div>
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="smtp-password">Password</label>
                  <input
                    id="smtp-password"
                    type={showPassword ? 'text' : 'password'}
                    autoComplete="new-password"
                    placeholder="Leave blank to keep the current password"
                    {...register('smtpPassword')}
                  />
                  <label className="form-check">
                    <input
                      type="checkbox"
                      checked={showPassword}
                      onChange={(e) => setShowPassword(e.target.checked)}
                    />
                    <span>Show password</span>
                  </label>
                </div>
                <div className="form-group">
                  <label htmlFor="smtp-from">From address</label>
                  <input id="smtp-from" type="text" {...register('smtpFrom')} />
                  {errors.smtpFrom ? <div className="field-error">{errors.smtpFrom.message}</div> : null}
                </div>
              </div>
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
