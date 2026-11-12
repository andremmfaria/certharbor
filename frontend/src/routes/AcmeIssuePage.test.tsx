import { QueryClient } from '@tanstack/react-query'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('acme issue page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('seeds the form from the default provider and issues on submit', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
    })
    renderRoutes(appRoutes, ['/acme/issue'])

    expect(await screen.findByText('HTTP-01')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByLabelText('Email')).toHaveValue('ops@example.test'))
    await userEvent.type(screen.getByLabelText('Domain'), 'my.example.test')
    await userEvent.click(screen.getByRole('button', { name: 'Issue Certificate' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/acme/certificates' && c.method === 'POST')).toBe(true),
    )
    const created = api.calls.find((c) => c.path === '/api/v1/acme/certificates' && c.method === 'POST')
    expect(created?.body).toMatchObject({
      domain: 'my.example.test',
      email: 'ops@example.test',
      provider: 'http-01',
      autoRenew: true,
    })
  })

  it('picks a different provider before submitting', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
    })
    renderRoutes(appRoutes, ['/acme/issue'])

    await screen.findByText('HTTP-01')
    await userEvent.click(screen.getByRole('button', { name: 'DNS-01' }))
    await userEvent.type(screen.getByLabelText('Domain'), 'dns.example.test')
    await userEvent.click(screen.getByRole('button', { name: 'Issue Certificate' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/acme/certificates' && c.method === 'POST')).toBe(true),
    )
    const created = api.calls.find((c) => c.path === '/api/v1/acme/certificates' && c.method === 'POST')
    expect(created?.body).toMatchObject({ provider: 'dns-01' })
  })

  it('shows a loading skeleton while options load', async () => {
    // Delay the response so the skeleton is still mounted once the route's
    // own HydrateFallback (Splash) has cleared, same pattern as
    // ProvisionersPage.test.tsx.
    installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
      delayMs: 20,
    })
    renderRoutes(appRoutes, ['/acme/issue'])

    const page = await screen.findByTestId('page-acme-issue')
    expect(within(page).getByTestId('skeleton')).toBeInTheDocument()
    await within(page).findByText('HTTP-01')
  })

  it('shows an error state when options fail to load', async () => {
    installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
      acmeOptionsDown: true,
    })
    // The default query client retries server errors with real exponential
    // backoff, which outlasts the test's assertion timeout; disable retry.
    renderRoutes(
      appRoutes,
      ['/acme/issue'],
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    )

    expect(await screen.findByTestId('error-state')).toBeInTheDocument()
  })

  it('is refused to a viewer', async () => {
    installFakeApi({ session: 'authenticated', user: { id: 2, username: 'bob', role: 'viewer' } })
    renderRoutes(appRoutes, ['/acme/issue'])

    expect(await screen.findByText('Access denied')).toBeInTheDocument()
  })
})
