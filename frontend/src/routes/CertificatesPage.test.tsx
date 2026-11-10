import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('certificates page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('lists certificates and links to detail', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/certificates'])

    expect(await screen.findByText('example')).toBeInTheDocument()
    expect(screen.getByText('example.test')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'example' })).toHaveAttribute('href', '/certificates/1')
  })

  it('shows the CA panel with its subject and fingerprint', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/certificates'])

    await screen.findByTestId('ca-panel')
    await waitFor(() => expect(screen.getByTestId('ca-panel')).toHaveTextContent('CN=Fake Root CA'))
  })

  it('hides manage actions and the CA chain downloads from a viewer', async () => {
    installFakeApi({ session: 'authenticated', user: { id: 2, username: 'bob', role: 'viewer' } })
    renderRoutes(appRoutes, ['/certificates'])

    await screen.findByText('example')
    expect(screen.queryByRole('button', { name: 'Renew' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Download Root' })).toBeNull()
    expect(screen.queryByRole('link', { name: 'Issue Certificate' })).toBeNull()
  })

  it('offers renew to a manager and revoke to an admin only', async () => {
    const manager = installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
    })
    renderRoutes(appRoutes, ['/certificates'])

    await screen.findByText('example')
    expect(screen.getByRole('button', { name: 'Renew' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Revoke' })).toBeNull()
    expect(manager).toBeDefined()
  })

  it('renews a certificate and shows a success toast', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
    })
    renderRoutes(appRoutes, ['/certificates'])

    await userEvent.click(await screen.findByRole('button', { name: 'Renew' }))

    expect(await screen.findByText('example renewed.')).toBeInTheDocument()
    expect(api.calls.some((c) => c.path === '/api/v1/certificates/1/renew')).toBe(true)
  })

  it('revokes an active certificate as admin after confirming', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const api = installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/certificates'])

    await userEvent.click(await screen.findByRole('button', { name: 'Revoke' }))

    await waitFor(() => expect(api.calls.some((c) => c.path === '/api/v1/certificates/1/revoke')).toBe(true))
    expect(confirm).toHaveBeenCalled()
  })

  it('filters by the search box', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      certificates: [
        {
          id: 1,
          name: 'alpha',
          domain: 'a.test',
          status: 'active',
          keyType: 'EC:P-256',
          serial: 'A',
          expiryStatus: 'ok',
          issuedAt: '2026-01-01T00:00:00Z',
          expiresAt: '2027-01-01T00:00:00Z',
        },
        {
          id: 2,
          name: 'beta',
          domain: 'b.test',
          status: 'active',
          keyType: 'EC:P-256',
          serial: 'B',
          expiryStatus: 'ok',
          issuedAt: '2026-01-01T00:00:00Z',
          expiresAt: '2027-01-01T00:00:00Z',
        },
      ],
    })
    renderRoutes(appRoutes, ['/certificates'])

    await screen.findByText('alpha')
    await userEvent.type(screen.getByLabelText('Search'), 'beta')

    await waitFor(() => expect(screen.queryByText('alpha')).toBeNull())
    expect(await screen.findByText('beta')).toBeInTheDocument()
    expect(api.calls.some((c) => c.path === '/api/v1/certificates' && c.method === 'GET')).toBe(true)
  })

  it('shows an empty state when no certificates match', async () => {
    installFakeApi({ session: 'authenticated', certificates: [] })
    renderRoutes(appRoutes, ['/certificates'])

    expect(await screen.findByTestId('empty-state')).toBeInTheDocument()
  })
})
