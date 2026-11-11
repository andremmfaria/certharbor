import { QueryClient } from '@tanstack/react-query'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('security page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('lists security events', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/security'])

    expect(await screen.findByText('alice')).toBeInTheDocument()
    expect(screen.getByText('203.0.113.9')).toBeInTheDocument()
    expect(screen.getByText('failure', { selector: 'span' })).toBeInTheDocument()
  })

  it('shows an empty state when there are no events', async () => {
    installFakeApi({ session: 'authenticated', securityEvents: [] })
    renderRoutes(appRoutes, ['/admin/security'])

    expect(await screen.findByTestId('empty-state')).toBeInTheDocument()
  })

  it('shows an error state and can retry', async () => {
    // Dedicated securityEventsDown flag: the shared `down` flag also breaks
    // the session/config calls every page needs just to render, leaving the
    // test stuck at the Splash screen forever.
    installFakeApi({ session: 'authenticated', securityEventsDown: true })
    renderRoutes(
      appRoutes,
      ['/admin/security'],
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    )

    expect(await screen.findByTestId('error-state')).toBeInTheDocument()
  })

  it('filters by outcome', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      securityEvents: [
        {
          id: 1,
          username: 'alice',
          ip: '203.0.113.9',
          outcome: 'failure',
          reason: 'bad password',
          createdAt: '2026-01-01T00:00:00Z',
        },
        {
          id: 2,
          username: 'bob',
          ip: '198.51.100.4',
          outcome: 'success',
          reason: 'login',
          createdAt: '2026-01-02T00:00:00Z',
        },
      ],
    })
    renderRoutes(appRoutes, ['/admin/security'])

    await screen.findByText('alice')
    await userEvent.selectOptions(screen.getByLabelText('Outcome'), 'failure')

    await waitFor(() => expect(screen.queryByText('bob')).toBeNull())
    expect(screen.getByText('alice')).toBeInTheDocument()
    expect(api.calls.some((c) => c.path === '/api/v1/security/events' && c.method === 'GET')).toBe(true)
  })

  it('filters by the search box', async () => {
    installFakeApi({
      session: 'authenticated',
      securityEvents: [
        {
          id: 1,
          username: 'alice',
          ip: '203.0.113.9',
          outcome: 'failure',
          reason: 'bad password',
          createdAt: '2026-01-01T00:00:00Z',
        },
        {
          id: 2,
          username: 'bob',
          ip: '198.51.100.4',
          outcome: 'failure',
          reason: 'bad password',
          createdAt: '2026-01-02T00:00:00Z',
        },
      ],
    })
    renderRoutes(appRoutes, ['/admin/security'])

    await screen.findByText('alice')
    await userEvent.type(screen.getByLabelText('Search'), 'bob')

    await waitFor(() => expect(screen.queryByText('alice')).toBeNull())
    expect(await screen.findByText('bob')).toBeInTheDocument()
  })

  it('unblocks the IP of a failed login row', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      securityEvents: [
        {
          id: 1,
          username: 'alice',
          ip: '203.0.113.9',
          outcome: 'failure',
          reason: 'bad password',
          createdAt: '2026-01-01T00:00:00Z',
        },
      ],
    })
    renderRoutes(appRoutes, ['/admin/security'])

    await userEvent.click(await screen.findByRole('button', { name: 'Unblock IP' }))

    await waitFor(() =>
      expect(
        api.calls.some((c) => c.path === '/api/v1/security/ip-blocks/203.0.113.9' && c.method === 'DELETE'),
      ).toBe(true),
    )
    expect(await screen.findByText('203.0.113.9 unblocked.')).toBeInTheDocument()
  })

  it('offers no unblock action on a successful login row', async () => {
    installFakeApi({
      session: 'authenticated',
      securityEvents: [
        {
          id: 1,
          username: 'alice',
          ip: '203.0.113.9',
          outcome: 'success',
          reason: 'login',
          createdAt: '2026-01-01T00:00:00Z',
        },
      ],
    })
    renderRoutes(appRoutes, ['/admin/security'])

    await screen.findByText('alice')
    expect(screen.queryByRole('button', { name: 'Unblock IP' })).toBeNull()
  })
})
