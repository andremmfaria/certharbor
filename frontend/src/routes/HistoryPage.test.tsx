import { QueryClient } from '@tanstack/react-query'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('history page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('lists history entries', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/history'])

    expect(await screen.findByText('example')).toBeInTheDocument()
    expect(screen.getByText('example.test')).toBeInTheDocument()
    expect(screen.getByText('issue', { selector: 'span' })).toBeInTheDocument()
  })

  it('shows an empty state when there are no entries', async () => {
    installFakeApi({ session: 'authenticated', history: [] })
    renderRoutes(appRoutes, ['/history'])

    expect(await screen.findByTestId('empty-state')).toBeInTheDocument()
  })

  it('shows an error state and can retry', async () => {
    // Use the narrow historyDown flag: the global `down` flag also breaks
    // the session/config calls every page needs just to render, leaving
    // the test stuck at the Splash screen forever.
    installFakeApi({ session: 'authenticated', historyDown: true })
    // The default query client retries server errors with real exponential
    // backoff, which outlasts the test's assertion timeout; disable retry.
    renderRoutes(appRoutes, ['/history'], new QueryClient({ defaultOptions: { queries: { retry: false } } }))

    expect(await screen.findByTestId('error-state')).toBeInTheDocument()
  })

  it('filters by action', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      history: [
        {
          id: 1,
          action: 'issue',
          certName: 'alpha',
          domain: 'a.test',
          details: '',
          username: 'alice',
          role: 'admin',
          createdAt: '2026-01-01T00:00:00Z',
        },
        {
          id: 2,
          action: 'revoke',
          certName: 'beta',
          domain: 'b.test',
          details: '',
          username: 'alice',
          role: 'admin',
          createdAt: '2026-01-02T00:00:00Z',
        },
      ],
    })
    renderRoutes(appRoutes, ['/history'])

    await screen.findByText('alpha')
    await userEvent.click(screen.getByRole('button', { name: 'issue' }))

    await waitFor(() => expect(screen.queryByText('beta')).toBeNull())
    expect(screen.getByText('alpha')).toBeInTheDocument()
    expect(api.calls.some((c) => c.path === '/api/v1/history' && c.method === 'GET')).toBe(true)
  })

  it('filters by certificate name', async () => {
    installFakeApi({
      session: 'authenticated',
      history: [
        {
          id: 1,
          action: 'issue',
          certName: 'alpha',
          domain: 'a.test',
          details: '',
          username: 'alice',
          role: 'admin',
          createdAt: '2026-01-01T00:00:00Z',
        },
        {
          id: 2,
          action: 'issue',
          certName: 'beta',
          domain: 'b.test',
          details: '',
          username: 'alice',
          role: 'admin',
          createdAt: '2026-01-02T00:00:00Z',
        },
      ],
    })
    renderRoutes(appRoutes, ['/history'])

    await screen.findByText('alpha')
    await userEvent.type(screen.getByLabelText('Certificate'), 'beta')

    await waitFor(() => expect(screen.queryByText('alpha')).toBeNull())
    expect(await screen.findByText('beta')).toBeInTheDocument()
  })
})
