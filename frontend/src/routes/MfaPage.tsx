import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useBlobObjectUrl } from '../api/blobObjectUrl'
import { errorMessage, errorStatus } from '../api/errors'
import {
  useConfirmMfaEnrollment,
  useDisableMfa,
  useMfaEnrollmentQr,
  useMfaStatus,
  useStartMfaEnrollment,
} from '../api/mfa'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'

interface CodeForm {
  code: string
}

interface DisableForm {
  currentPassword: string
  code: string
}

// The enrollment step: shows the QR (as a blob object URL — the CSP's
// img-src must allow blob: for this to render) plus the manual-entry key
// fallback from startMfaEnrollment's response, then a TOTP confirm form.
function EnrollmentStep({
  manualEntryKey,
  onConfirmed,
}: {
  manualEntryKey?: string
  onConfirmed: (codes: string[]) => void
}) {
  const qr = useMfaEnrollmentQr(true)
  const qrUrl = useBlobObjectUrl(qr.data instanceof Blob ? qr.data : undefined)
  const confirm = useConfirmMfaEnrollment()
  const [formError, setFormError] = useState<string | undefined>()
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<CodeForm>()

  const onSubmit = handleSubmit((values) => {
    setFormError(undefined)
    confirm.mutate(
      { body: { code: values.code } },
      {
        onSuccess: (body) => onConfirmed(body.recoveryCodes ?? []),
        // Fixed message, not the server's detail: same convention as
        // DisableMfaForm below. The 422 detail text is an internal
        // validation-error string ("invalid TOTP code" / "no enrollment is
        // pending"), not something to surface verbatim to the user.
        onError: () => {
          setFormError('That code was not accepted.')
          reset()
        },
      },
    )
  })

  return (
    <div className="card" data-testid="mfa-enrollment">
      <div className="card-header">
        <h3>Set up two-factor authentication</h3>
      </div>
      <div className="card-body form-grid">
        <p>Scan this QR code with your authenticator app:</p>
        {qr.isPending ? (
          <Skeleton rows={3} label="Loading QR code" />
        ) : qr.isError ? (
          <ErrorState problem={qr.error} onRetry={() => void qr.refetch()} />
        ) : qrUrl ? (
          <img src={qrUrl} alt="Scan this QR code with your authenticator app" width={200} height={200} />
        ) : null}

        {manualEntryKey ? (
          <div className="form-group">
            <label>Or enter this key manually</label>
            <code data-testid="mfa-manual-entry-key">{manualEntryKey}</code>
          </div>
        ) : null}

        <form onSubmit={onSubmit} noValidate>
          <div className="form-group">
            <label htmlFor="mfa-confirm-code">Code from your authenticator app</label>
            <input
              id="mfa-confirm-code"
              type="text"
              autoFocus
              autoComplete="one-time-code"
              inputMode="numeric"
              placeholder="123456"
              {...register('code', { required: 'Enter a code' })}
            />
            {errors.code ? <div className="field-error">{errors.code.message}</div> : null}
          </div>
          {formError ? (
            <div className="error" role="alert" data-testid="mfa-confirm-error">
              {formError}
            </div>
          ) : null}
          <button className="btn" type="submit" disabled={confirm.isPending}>
            Confirm
          </button>
        </form>
      </div>
    </div>
  )
}

// Recovery codes are returned exactly once, by confirmMfaEnrollment's
// response (the server never stores them in a retrievable form). Shown
// here with copy-to-clipboard; dismissing is a one-way local action, not a
// server call, because there is nothing left on the server for it to ack.
function RecoveryCodesStep({ codes, onDismiss }: { codes: string[]; onDismiss: () => void }) {
  const toast = useToast()
  const copyAll = () => {
    void navigator.clipboard.writeText(codes.join('\n'))
    toast.success('Recovery codes copied to clipboard.')
  }
  return (
    <div className="card" data-testid="mfa-recovery-codes">
      <div className="card-header">
        <h3>Save your recovery codes</h3>
      </div>
      <div className="card-body">
        <p>
          Store these codes somewhere safe. Each can be used once to sign in if you lose access to your
          authenticator app. <strong>They will not be shown again.</strong>
        </p>
        <ul className="recovery-codes">
          {codes.map((c) => (
            <li key={c}>
              <code>{c}</code>
            </li>
          ))}
        </ul>
        <div className="page-actions">
          <button type="button" className="btn-sm" onClick={copyAll}>
            Copy all
          </button>
          <button type="button" className="btn" onClick={onDismiss}>
            I've saved these codes
          </button>
        </div>
      </div>
    </div>
  )
}

function DisableMfaForm() {
  const disable = useDisableMfa()
  const [formError, setFormError] = useState<string | undefined>()
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<DisableForm>()

  const onSubmit = handleSubmit((values) => {
    setFormError(undefined)
    disable.mutate(
      { body: values },
      {
        onError: (e) => {
          // 401: the current password was wrong (a credential problem). 422: the
          // password was right but the TOTP code was not (a different reauth
          // factor failing) — api/profile_mfa.go's disableMfa draws this line.
          const status = errorStatus(e)
          if (status === 401) setFormError('Current password is incorrect.')
          else if (status === 422) setFormError('That authenticator code was not accepted.')
          else setFormError(errorMessage(e, 'Disabling two-factor authentication failed.'))
          reset({ code: '' }, { keepValues: true, keepErrors: false })
        },
      },
    )
  })

  return (
    <div className="card" data-testid="mfa-disable">
      <div className="card-header">
        <h3>Disable two-factor authentication</h3>
      </div>
      <div className="card-body">
        <form onSubmit={onSubmit} noValidate className="form-grid">
          <div className="form-group">
            <label htmlFor="mfa-disable-password">Current password</label>
            <input
              id="mfa-disable-password"
              type="password"
              autoComplete="current-password"
              {...register('currentPassword', { required: 'Current password is required' })}
            />
            {errors.currentPassword ? (
              <div className="field-error">{errors.currentPassword.message}</div>
            ) : null}
          </div>
          <div className="form-group">
            <label htmlFor="mfa-disable-code">Authenticator code</label>
            <input
              id="mfa-disable-code"
              type="text"
              autoComplete="one-time-code"
              inputMode="numeric"
              placeholder="123456"
              {...register('code', { required: 'Enter a code' })}
            />
            {errors.code ? <div className="field-error">{errors.code.message}</div> : null}
          </div>
          {formError ? (
            <div className="error" role="alert" data-testid="mfa-disable-error">
              {formError}
            </div>
          ) : null}
          <div className="page-actions">
            <button className="btn btn-danger" type="submit" disabled={disable.isPending}>
              Disable 2FA
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export function MfaPage() {
  const status = useMfaStatus()
  const startEnrollment = useStartMfaEnrollment()
  const [manualEntryKey, setManualEntryKey] = useState<string | undefined>()
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | undefined>()
  const [enrolling, setEnrolling] = useState(false)

  const onStart = () => {
    startEnrollment.mutate(
      {},
      {
        onSuccess: (body) => {
          setManualEntryKey(body.manualEntryKey)
          setEnrolling(true)
        },
      },
    )
  }

  return (
    <section data-testid="page-profile-mfa">
      <div className="page-header">
        <div>
          <div className="breadcrumb">profile &middot; two-factor authentication</div>
          <h1>Two-factor authentication</h1>
        </div>
      </div>

      {status.isPending ? (
        <Skeleton rows={3} label="Loading two-factor authentication status" />
      ) : status.isError ? (
        <ErrorState problem={status.error} onRetry={() => void status.refetch()} />
      ) : recoveryCodes ? (
        <RecoveryCodesStep
          codes={recoveryCodes}
          onDismiss={() => {
            setRecoveryCodes(undefined)
            setEnrolling(false)
            setManualEntryKey(undefined)
          }}
        />
      ) : enrolling || status.data?.pendingEnrollment ? (
        <EnrollmentStep manualEntryKey={manualEntryKey} onConfirmed={(codes) => setRecoveryCodes(codes)} />
      ) : status.data?.enabled ? (
        <>
          <div className="alert alert-ok" role="status">
            Two-factor authentication is enabled on this account.
          </div>
          <DisableMfaForm />
        </>
      ) : (
        <div className="card">
          <div className="card-body">
            <p>Two-factor authentication is not enabled on this account.</p>
            <button className="btn" type="button" onClick={onStart} disabled={startEnrollment.isPending}>
              Enable 2FA
            </button>
          </div>
        </div>
      )}
    </section>
  )
}
