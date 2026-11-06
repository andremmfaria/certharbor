import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { appRoutes } from '../routes/appRoutes'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'

describe('login view', () => {
  let eject: () => void
  const handlers = { clearCache: vi.fn(), redirectToLogin: vi.fn() }
  beforeEach(() => {
    eject = installClient(handlers)
  })
  afterEach(() => eject())

  async function signIn(password: string) {
    await userEvent.type(await screen.findByLabelText('Username'), 'alice')
    await userEvent.type(screen.getByLabelText('Password'), password)
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }))
  }

  it('renders a field-level refusal for a wrong password rather than resetting the page', async () => {
    installFakeApi({ password: 'right' })
    const { queryClient } = renderRoutes(appRoutes, ['/login'])
    const clear = vi.spyOn(queryClient, 'clear')

    await signIn('wrong')

    expect(await screen.findByTestId('login-error')).toHaveTextContent('Invalid credentials')
    expect(screen.getByLabelText('Username')).toHaveValue('alice')
    expect(clear).not.toHaveBeenCalled()
    expect(handlers.redirectToLogin).not.toHaveBeenCalled()
  })

  it('validates empty fields before sending anything', async () => {
    const api = installFakeApi()
    renderRoutes(appRoutes, ['/login'])

    await userEvent.click(await screen.findByRole('button', { name: /sign in/i }))

    expect(await screen.findByText('Username is required')).toBeInTheDocument()
    expect(api.calls.filter((c) => c.method === 'POST')).toHaveLength(0)
  })

  it('sends the CSRF cookie with the credentials', async () => {
    const api = installFakeApi({ password: 'pw', csrfCookie: 'the-token' })
    renderRoutes(appRoutes, ['/login'])

    await signIn('pw')

    await waitFor(() => expect(api.calls.find((c) => c.method === 'POST')?.csrf).toBe('the-token'))
  })

  it('takes the second step when the account has MFA and completes it with a code', async () => {
    const api = installFakeApi({ password: 'pw', mfaRequired: true })
    renderRoutes(appRoutes, ['/login'])

    await signIn('pw')
    await userEvent.type(await screen.findByLabelText('2FA Code'), '123456')
    await userEvent.click(screen.getByRole('button', { name: /confirm/i }))

    expect(await screen.findByTestId('page-dashboard')).toBeInTheDocument()
    expect(api.calls.find((c) => c.path === '/api/v1/session/mfa')?.body).toEqual({
      code: '123456',
      codeType: 'totp',
    })
  })

  it('resumes on the code step after a refresh mid-MFA', async () => {
    installFakeApi({ session: 'pendingMfa', mfaRequired: true })
    renderRoutes(appRoutes, ['/login'])

    expect(await screen.findByLabelText('2FA Code')).toBeInTheDocument()
    expect(screen.queryByLabelText('Password')).toBeNull()
  })

  it('submits a recovery code as such', async () => {
    const api = installFakeApi({ session: 'pendingMfa', mfaRequired: true })
    renderRoutes(appRoutes, ['/login'])

    await userEvent.click(await screen.findByRole('button', { name: /recovery code instead/i }))
    await userEvent.type(screen.getByLabelText('Recovery code'), api.state.recoveryCode)
    await userEvent.click(screen.getByRole('button', { name: /confirm/i }))

    expect(await screen.findByTestId('page-dashboard')).toBeInTheDocument()
    expect(api.calls.find((c) => c.path === '/api/v1/session/mfa')?.body).toMatchObject({
      codeType: 'recovery',
    })
  })

  it('shows a wrong code as an error and stays on the code step', async () => {
    installFakeApi({ session: 'pendingMfa', mfaRequired: true })
    renderRoutes(appRoutes, ['/login'])

    await userEvent.type(await screen.findByLabelText('2FA Code'), '000000')
    await userEvent.click(screen.getByRole('button', { name: /confirm/i }))

    expect(await screen.findByTestId('login-error')).toBeInTheDocument()
    expect(screen.getByLabelText('2FA Code')).toBeInTheDocument()
  })

  it('falls back to the credentials form when the pending sign-in has expired', async () => {
    const api = installFakeApi({ session: 'pendingMfa', mfaRequired: true })
    renderRoutes(appRoutes, ['/login'])
    const input = await screen.findByLabelText('2FA Code')

    api.state.session = 'anonymous'
    await userEvent.type(input, '123456')
    await userEvent.click(screen.getByRole('button', { name: /confirm/i }))

    expect(await screen.findByLabelText('Password')).toBeInTheDocument()
    expect(await screen.findByTestId('login-error')).toHaveTextContent('expired')
  })

  it('signs out and lands on the login view', async () => {
    const api = installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/certificates'])

    await userEvent.click((await screen.findAllByRole('button', { name: /sign out/i }))[0]!)

    expect(await screen.findByTestId('page-login')).toBeInTheDocument()
    expect(api.state.session).toBe('anonymous')
  })

  it('offers the SSO button and hides the form when only SSO is enabled', async () => {
    installFakeApi({ oidcEnabled: true, localLoginEnabled: false })
    renderRoutes(appRoutes, ['/login'])

    expect(await screen.findByRole('link', { name: 'Continue with SSO' })).toHaveAttribute(
      'href',
      '/auth/oidc/login',
    )
    expect(screen.queryByLabelText('Password')).toBeNull()
  })

  it('maps an error code to fixed text and ignores unknown ones', async () => {
    installFakeApi()
    renderRoutes(appRoutes, ['/login?error=%3Cb%3Ehi%3C%2Fb%3E'])

    expect(await screen.findByText('Single sign-on failed. Please try again.')).toBeInTheDocument()
    expect(screen.queryByText('hi')).toBeNull()
  })
})
