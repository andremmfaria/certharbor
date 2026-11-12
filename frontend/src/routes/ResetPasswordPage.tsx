import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link, useSearchParams } from 'react-router'
import { z } from 'zod'
import { errorMessage } from '../api/errors'
import { useConfirmPasswordReset, useValidatePasswordReset } from '../api/queries'
import { problemToFieldErrors } from '../api/problemToFieldErrors'
import { isProblem } from '../api/errors'
import { Skeleton } from '../components/Skeleton'

const newPasswordSchema = z.object({
  newPassword: z.string().min(1, 'New password is required'),
})
type NewPasswordForm = z.infer<typeof newPasswordSchema>

// Reads ?token= once, synchronously, from a lazy useState initializer (so it
// runs exactly once on mount, before paint) and immediately strips it from
// the visible address bar via history.replaceState -- not useSearchParams'
// setter, so the browser's address bar and history entry both drop the
// token right away rather than leaving it live in a reload/back-button/
// shoulder-surfing window. The captured value then lives in state, not a
// ref: the React Compiler forbids reading ref.current during render, and
// this value is read on every render to decide what to show.
// validatePasswordReset checks the token is live before showing the new
// password form, without consuming it (backend/api/password_reset.go), so a
// link scanner or mail prefetcher loading this page does not burn the
// token before the real recipient submits it.
export function ResetPasswordPage() {
  const [params] = useSearchParams()
  const [token] = useState<string>(() => {
    const value = params.get('token') ?? ''
    const url = new URL(window.location.href)
    url.searchParams.delete('token')
    window.history.replaceState(window.history.state, '', url.pathname + url.search + url.hash)
    return value
  })
  const validate = useValidatePasswordReset()
  const confirm = useConfirmPasswordReset()
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<NewPasswordForm>({ resolver: zodResolver(newPasswordSchema) })

  useEffect(() => {
    if (!token) return
    validate.mutate({ body: { token } })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token])

  const onSubmit = handleSubmit((values) => {
    confirm.mutate(
      { body: { token, newPassword: values.newPassword } },
      {
        onError: (problem) => {
          const fields = isProblem(problem) ? problemToFieldErrors(problem) : {}
          const names = Object.keys(fields)
          if (names.length > 0) {
            for (const name of names) setError(name as keyof NewPasswordForm, { message: fields[name] })
          }
        },
      },
    )
  })

  if (!token || validate.isError) {
    return (
      <div className="card" data-testid="page-reset-password">
        <h2>Reset password</h2>
        <div className="error" role="alert" data-testid="reset-password-token-error">
          This reset link is invalid or has expired. Please request a new one.
        </div>
        <div className="auth-footer">
          <Link className="auth-link" to="/forgot-password">
            Request a new link
          </Link>
        </div>
      </div>
    )
  }

  if (validate.isIdle || validate.isPending) {
    return (
      <div className="card" data-testid="page-reset-password">
        <Skeleton rows={3} label="Checking your link" />
      </div>
    )
  }

  if (confirm.isSuccess) {
    return (
      <div className="card" data-testid="page-reset-password">
        <h2>Password updated</h2>
        <p>Your password has been changed. You can now sign in with your new password.</p>
        <div className="auth-footer">
          <Link className="auth-link" to="/login">
            Back to sign in
          </Link>
        </div>
      </div>
    )
  }

  return (
    <div className="card" data-testid="page-reset-password">
      <h2>Choose a new password</h2>
      <form onSubmit={onSubmit} noValidate>
        <div className="form-group">
          <label htmlFor="newPassword">New password</label>
          <input
            id="newPassword"
            type="password"
            autoComplete="new-password"
            autoFocus
            {...register('newPassword')}
          />
          {errors.newPassword ? <div className="field-error">{errors.newPassword.message}</div> : null}
        </div>
        {confirm.isError && Object.keys(errors).length === 0 ? (
          <div className="error" role="alert" data-testid="reset-password-error">
            {errorMessage(confirm.error, 'Could not reset your password. Please try again.')}
          </div>
        ) : null}
        <button className="btn" type="submit" disabled={confirm.isPending}>
          Set new password &rarr;
        </button>
      </form>
    </div>
  )
}
