import { QueryClient } from '@tanstack/react-query'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('acme certificates page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('lists ACME certificates', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/acme'])

    expect(await screen.findByText('acme.example.test')).toBeInTheDocument()
    expect(screen.getByText('ops@example.test')).toBeInTheDocument()
    expect(screen.getByText('http-01')).toBeInTheDocument()
  })

  it('is refused to a viewer (route requires manager)', async () => {
    installFakeApi({ session: 'authenticated', user: { id: 2, username: 'bob', role: 'viewer' } })
    renderRoutes(appRoutes, ['/acme'])

    expect(await screen.findByText('Access denied')).toBeInTheDocument()
  })

  it('renews a certificate and shows a success toast', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
    })
    renderRoutes(appRoutes, ['/acme'])

    await userEvent.click(await screen.findByRole('button', { name: 'Renew' }))

    expect(await screen.findByText('acme.example.test renewed.')).toBeInTheDocument()
    expect(api.calls.some((c) => c.path === '/api/v1/acme/certificates/1/renew')).toBe(true)
  })

  it('deletes a certificate after confirming', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const api = installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
    })
    renderRoutes(appRoutes, ['/acme'])

    await userEvent.click(await screen.findByRole('button', { name: 'Delete' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/acme/certificates/1' && c.method === 'DELETE')).toBe(
        true,
      ),
    )
    expect(confirm).toHaveBeenCalled()
  })

  it('toggles auto-renew for a manager', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
    })
    renderRoutes(appRoutes, ['/acme'])

    await userEvent.click(await screen.findByRole('button', { name: 'On' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/acme/certificates/1' && c.method === 'PATCH')).toBe(
        true,
      ),
    )
    const patchCall = api.calls.find((c) => c.path === '/api/v1/acme/certificates/1' && c.method === 'PATCH')
    expect(patchCall?.body).toMatchObject({ autoRenew: false })
  })

  it('downloads the certificate and key files', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
    })
    renderRoutes(appRoutes, ['/acme'])

    await userEvent.click(await screen.findByRole('button', { name: 'CRT' }))
    await userEvent.click(await screen.findByRole('button', { name: 'KEY' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/acme/certificates/1/certificate')).toBe(true),
    )
    expect(api.calls.some((c) => c.path === '/api/v1/acme/certificates/1/key')).toBe(true)
  })

  it('shows an empty state when there are no ACME certificates', async () => {
    installFakeApi({ session: 'authenticated', acmeCertificates: [] })
    renderRoutes(appRoutes, ['/acme'])

    expect(await screen.findByTestId('empty-state')).toBeInTheDocument()
  })

  it('shows an error state and allows a retry', async () => {
    installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
      acmeCertificatesDown: true,
    })
    // The default query client retries server errors with real exponential
    // backoff, which outlasts the test's assertion timeout; disable retry.
    renderRoutes(appRoutes, ['/acme'], new QueryClient({ defaultOptions: { queries: { retry: false } } }))

    expect(await screen.findByRole('button', { name: /try again/i })).toBeInTheDocument()
  })
})
