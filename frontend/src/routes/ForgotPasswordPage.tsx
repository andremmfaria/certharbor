import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Link } from 'react-router'
import { z } from 'zod'
import { errorMessage } from '../api/errors'
import { useRequestPasswordReset } from '../api/queries'

const forgotPasswordSchema = z.object({
  email: z.string().trim().min(1, 'Email or username is required'),
})
type ForgotPasswordForm = z.infer<typeof forgotPasswordSchema>

// requestPasswordReset always answers 202, whether or not the account
// exists, is active, or has an email on file (backend/api/password_reset.go's
// no-enumeration guarantee). The UI must not branch on anything it could
// only learn from that 2xx body -- so a successful request always shows the
// same "check your email" copy. A thrown error here is a real transport or
// server failure (network down, 5xx, rate-limited), not an account-shaped
// signal, so it is fine to surface and let the caller retry.
export function ForgotPasswordPage() {
  const requestReset = useRequestPasswordReset()
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<ForgotPasswordForm>({ resolver: zodResolver(forgotPasswordSchema) })

  const onSubmit = handleSubmit((values) => {
    requestReset.mutate({ body: { email: values.email } })
  })

  if (requestReset.isSuccess) {
    return (
      <div className="card" data-testid="page-forgot-password">
        <h2>Check your email</h2>
        <p>
          If an account matches what you entered, we&rsquo;ve sent a link to reset the password. The link
          expires soon, so use it promptly.
        </p>
        <div className="auth-footer">
          <Link className="auth-link" to="/login">
            Back to sign in
          </Link>
        </div>
      </div>
    )
  }

  return (
    <div className="card" data-testid="page-forgot-password">
      <h2>Forgot password</h2>
      <p>Enter your account email or username and we&rsquo;ll send you a link to reset your password.</p>
      <form onSubmit={onSubmit} noValidate>
        <div className="form-group">
          <label htmlFor="email">Email or username</label>
          <input id="email" type="text" autoComplete="username" autoFocus {...register('email')} />
          {errors.email ? <div className="field-error">{errors.email.message}</div> : null}
        </div>
        {requestReset.isError ? (
          <div className="error" role="alert" data-testid="forgot-password-error">
            {errorMessage(requestReset.error, 'Something went wrong. Please try again.')}
          </div>
        ) : null}
        <button className="btn" type="submit" disabled={requestReset.isPending}>
          {requestReset.isPending ? 'Sending…' : <>Send reset link &rarr;</>}
        </button>
        <div className="auth-footer">
          <Link className="auth-link" to="/login">
            Back to sign in
          </Link>
        </div>
      </form>
    </div>
  )
}
