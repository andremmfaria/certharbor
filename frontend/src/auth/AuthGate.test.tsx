import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { appRoutes } from '../routes/appRoutes'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'

describe('AuthGate', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => {
    eject()
    vi.useRealTimers()
  })

  it('renders the protected route for an authenticated session', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/certificates'])

    expect(await screen.findByTestId('page-certificates')).toBeInTheDocument()
  })

  it('sends an anonymous visitor to the login view and keeps the deep link for afterwards', async () => {
    const api = installFakeApi({ session: 'anonymous', password: 'pw' })
    const { router } = renderRoutes(appRoutes, ['/certificates/7'])

    await screen.findByTestId('page-login')
    expect(router.state.location.pathname).toBe('/login')

    await userEvent.type(screen.getByLabelText('Username'), 'alice')
    await userEvent.type(screen.getByLabelText('Password'), 'pw')
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }))

    expect(await screen.findByTestId('page-certificate-detail')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/certificates/7')
    expect(api.state.session).toBe('authenticated')
  })

  it.each([
    [
      'a 503 problem document',
      { down: { status: 503, body: '{"status":503}', contentType: 'application/problem+json' } },
    ],
    [
      'an HTML 502 page',
      { down: { status: 502, body: '<html>Bad Gateway</html>', contentType: 'text/html' } },
    ],
    ['a refused connection', { down: 'network' as const }],
  ])('renders the unavailable state, never the login form, on %s', async (_name, down) => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    installFakeApi(down)
    renderRoutes(appRoutes, ['/certificates'])

    await vi.advanceTimersByTimeAsync(10_000)

    expect(await screen.findByTestId('unavailable')).toBeInTheDocument()
    expect(screen.queryByLabelText('Password')).toBeNull()
  })

  it('retries from the unavailable state and recovers', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const api = installFakeApi({ session: 'authenticated', down: 'network' })
    renderRoutes(appRoutes, ['/certificates'])
    await vi.advanceTimersByTimeAsync(10_000)
    await screen.findByTestId('unavailable')

    api.state.down = undefined
    await userEvent
      .setup({ advanceTimers: vi.advanceTimersByTime })
      .click(screen.getByRole('button', { name: /try again/i }))

    expect(await screen.findByTestId('page-certificates')).toBeInTheDocument()
  })

  it('does not retry a 4xx from the boot call', async () => {
    const api = installFakeApi({
      down: { status: 400, body: '{"status":400}', contentType: 'application/problem+json' },
    })
    renderRoutes(appRoutes, ['/certificates'])

    await screen.findByTestId('unavailable')
    expect(api.calls.filter((c) => c.path === '/api/v1/session')).toHaveLength(1)
  })
})
