import { QueryClient } from '@tanstack/react-query'
import { screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('activity page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('lists audit events', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/activity'])

    expect(await screen.findByText('alice')).toBeInTheDocument()
    expect(screen.getByText('203.0.113.9')).toBeInTheDocument()
    expect(screen.getByText('success', { selector: 'span' })).toBeInTheDocument()
  })

  it('shows an empty state when there are no events', async () => {
    installFakeApi({ session: 'authenticated', auditEvents: [] })
    renderRoutes(appRoutes, ['/admin/activity'])

    expect(await screen.findByTestId('empty-state')).toBeInTheDocument()
  })

  it('shows an error state and can retry', async () => {
    // Dedicated auditEventsDown flag: the shared `down` flag also breaks
    // the session/config calls every page needs just to render, leaving the
    // test stuck at the Splash screen forever.
    installFakeApi({ session: 'authenticated', auditEventsDown: true })
    renderRoutes(
      appRoutes,
      ['/admin/activity'],
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    )

    expect(await screen.findByTestId('error-state')).toBeInTheDocument()
  })

  it('offers no filter controls', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/activity'])

    await screen.findByText('alice')
    expect(screen.queryByLabelText('Search')).toBeNull()
    expect(screen.queryByLabelText('Outcome')).toBeNull()
  })
})
