import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link, Navigate, useLocation, useSearchParams } from 'react-router'
import { z } from 'zod'
import { hasProblemType, errorMessage, isProblem } from '../api/errors'
import { problemToFieldErrors } from '../api/problemToFieldErrors'
import { useConfig, useCreateSession, useSession, useSubmitMfa } from '../api/queries'
import { Skeleton } from '../components/Skeleton'
import { oidcErrorText } from './oidcErrors'
import { useSignOut } from './useSignOut'
import { Unavailable } from './Unavailable'

const credentialsSchema = z.object({
  username: z.string().trim().min(1, 'Username is required'),
  password: z.string().min(1, 'Password is required'),
})
type Credentials = z.infer<typeof credentialsSchema>

const codeSchema = z.object({ code: z.string().trim().min(1, 'Enter a code') })
type CodeForm = z.infer<typeof codeSchema>

function CredentialsForm({ onExpired }: { onExpired?: string }) {
  const createSession = useCreateSession()
  const [formError, setFormError] = useState<string | undefined>(onExpired)
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<Credentials>({ resolver: zodResolver(credentialsSchema) })

  const onSubmit = handleSubmit((values) => {
    setFormError(undefined)
    createSession.mutate(
      { body: values },
      {
        onError: (problem) => {
          const fields = isProblem(problem) ? problemToFieldErrors(problem) : {}
          const names = Object.keys(fields)
          if (names.length > 0) {
            for (const name of names) setError(name as keyof Credentials, { message: fields[name] })
            return
          }
          setFormError(errorMessage(problem, 'Sign in failed. Please try again.'))
        },
      },
    )
  })

  return (
    <form onSubmit={onSubmit} noValidate>
      <div className="form-group">
        <label htmlFor="username">Username</label>
        <input id="username" type="text" autoComplete="username" autoFocus {...register('username')} />
        {errors.username ? <div className="field-error">{errors.username.message}</div> : null}
      </div>
      <div className="form-group">
        <label htmlFor="password">Password</label>
        <input id="password" type="password" autoComplete="current-password" {...register('password')} />
        {errors.password ? <div className="field-error">{errors.password.message}</div> : null}
      </div>
      {formError ? (
        <div className="error" role="alert" data-testid="login-error">
          {formError}
        </div>
      ) : null}
      <button className="btn" type="submit" disabled={createSession.isPending}>
        Sign In &rarr;
      </button>
      <div className="auth-footer">
        <Link className="auth-link" to="/forgot-password">
          Forgot password?
        </Link>
      </div>
    </form>
  )
}

function CodeStep({ onRestart }: { onRestart: () => void }) {
  const submitMfa = useSubmitMfa()
  const signOut = useSignOut()
  const session = useSession()
  const [recovery, setRecovery] = useState(false)
  const [formError, setFormError] = useState<string | undefined>()
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<CodeForm>({ resolver: zodResolver(codeSchema) })

  const onSubmit = handleSubmit((values) => {
    setFormError(undefined)
    submitMfa.mutate(
      { body: { code: values.code, codeType: recovery ? 'recovery' : 'totp' } },
      {
        onError: (problem) => {
          if (hasProblemType(problem, 'mfa-expired')) {
            onRestart()
            void session.refetch()
            return
          }
          setFormError(errorMessage(problem, 'That code was not accepted.'))
          reset()
        },
      },
    )
  })

  return (
    <form onSubmit={onSubmit} noValidate>
      <div className="form-group">
        <label htmlFor="code">{recovery ? 'Recovery code' : '2FA Code'}</label>
        <input
          id="code"
          type="text"
          autoFocus
          autoComplete={recovery ? 'off' : 'one-time-code'}
          inputMode={recovery ? 'text' : 'numeric'}
          placeholder={recovery ? 'XXXXXX-XXXXXX-XXXXXX' : '123456'}
          {...register('code')}
        />
        {errors.code ? <div className="field-error">{errors.code.message}</div> : null}
      </div>
      {formError ? (
        <div className="error" role="alert" data-testid="login-error">
          {formError}
        </div>
      ) : null}
      <button className="btn" type="submit" disabled={submitMfa.isPending}>
        Confirm &rarr;
      </button>
      <div className="auth-footer">
        <button type="button" className="auth-link auth-link-button" onClick={() => setRecovery(!recovery)}>
          {recovery ? 'Use the authenticator app instead' : 'Use a recovery code instead'}
        </button>
      </div>
      <div className="auth-footer">
        <button type="button" className="auth-link auth-link-button" onClick={signOut}>
          Cancel and start again
        </button>
      </div>
    </form>
  )
}

export function LoginPage() {
  const session = useSession()
  const config = useConfig(!session.isPending)
  const location = useLocation()
  const [params] = useSearchParams()
  const [expired, setExpired] = useState<string | undefined>()

  if (session.isPending) return <Skeleton rows={4} label="Loading" />
  if (!session.data) return <Unavailable onRetry={() => void session.refetch()} />

  if (session.data.state === 'authenticated') {
    const from = (location.state as { from?: { pathname: string; search: string } } | null)?.from
    return <Navigate to={from ? from.pathname + from.search : '/'} replace />
  }

  const oidcError = oidcErrorText(params.get('error'))

  return (
    <div className="card" data-testid="page-login">
      <h2>Sign In</h2>
      {oidcError ? <div className="error">{oidcError}</div> : null}
      {!config.data ? (
        <Skeleton rows={3} label="Loading" />
      ) : (
        <>
          {config.data.oidcEnabled ? (
            <a className="btn-oidc" href="/auth/oidc/login">
              {config.data.oidcButtonLabel || 'Single sign-on'}
            </a>
          ) : null}
          {config.data.localLoginEnabled ? (
            <>
              {config.data.oidcEnabled ? <div className="divider">or</div> : null}
              {session.data.state === 'pendingMfa' ? (
                <CodeStep onRestart={() => setExpired('Your sign-in expired. Please start again.')} />
              ) : (
                <CredentialsForm onExpired={expired} />
              )}
            </>
          ) : config.data.oidcEnabled ? null : (
            <div className="error">Local login is disabled.</div>
          )}
        </>
      )}
    </div>
  )
}
