import { QueryClient } from '@tanstack/react-query'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('acme events page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('lists acme events', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/le/logs'])

    expect(await screen.findByText('acme.example.test')).toBeInTheDocument()
    expect(screen.getByText('issue', { selector: 'span' })).toBeInTheDocument()
    expect(screen.getByText('Certificate issued successfully')).toBeInTheDocument()
  })

  it('shows an empty state when there are no events', async () => {
    installFakeApi({ session: 'authenticated', acmeEvents: [] })
    renderRoutes(appRoutes, ['/le/logs'])

    expect(await screen.findByTestId('empty-state')).toBeInTheDocument()
  })

  it('shows a loading skeleton while pending', async () => {
    installFakeApi({ session: 'authenticated', delayMs: 20 })
    renderRoutes(appRoutes, ['/le/logs'])

    const page = await screen.findByTestId('page-acme-logs')
    expect(within(page).getByTestId('skeleton')).toBeInTheDocument()
    await within(page).findByText('acme.example.test')
  })

  it('shows an error state and can retry', async () => {
    // Dedicated acmeEventsDown flag: the shared `down` flag also breaks
    // the session/config calls every page needs just to render, leaving
    // the test stuck at the Splash screen forever.
    installFakeApi({ session: 'authenticated', acmeEventsDown: true })
    renderRoutes(appRoutes, ['/le/logs'], new QueryClient({ defaultOptions: { queries: { retry: false } } }))

    expect(await screen.findByTestId('error-state')).toBeInTheDocument()
  })

  it('filters by domain', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      acmeEvents: [
        {
          id: 1,
          domain: 'alpha.example.test',
          action: 'issue',
          message: 'Certificate issued successfully',
          createdAt: '2026-01-01T00:00:00Z',
        },
        {
          id: 2,
          domain: 'beta.example.test',
          action: 'renew',
          message: 'Certificate renewed',
          createdAt: '2026-01-02T00:00:00Z',
        },
      ],
    })
    renderRoutes(appRoutes, ['/le/logs'])

    await screen.findByText('alpha.example.test')
    await userEvent.type(screen.getByLabelText('Domain'), 'beta.example.test')

    await waitFor(() => expect(screen.queryByText('alpha.example.test')).toBeNull())
    expect(await screen.findByText('beta.example.test')).toBeInTheDocument()
    expect(api.calls.some((c) => c.path === '/api/v1/acme/events' && c.method === 'GET')).toBe(true)
  })

  it('paginates through multiple pages', async () => {
    const events = Array.from({ length: 30 }, (_, i) => ({
      id: i + 1,
      domain: `d${i + 1}.example.test`,
      action: 'issue' as const,
      message: 'Certificate issued successfully',
      createdAt: '2026-01-01T00:00:00Z',
    }))
    installFakeApi({ session: 'authenticated', acmeEvents: events })
    renderRoutes(appRoutes, ['/le/logs'])

    expect(await screen.findByText('d1.example.test')).toBeInTheDocument()
    expect(screen.getByText('Page 1 of 2')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled()

    await userEvent.click(screen.getByRole('button', { name: 'Next' }))

    await waitFor(() => expect(screen.getByText('Page 2 of 2')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled()
  })
})
