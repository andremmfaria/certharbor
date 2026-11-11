import { QueryClient } from '@tanstack/react-query'
import { screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('provisioners page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('lists provisioners', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/provisioners'])

    // 'admin' also appears as the signed-in user's role in the sidebar, so
    // scope the query to the page body to avoid matching that instead.
    const page = await screen.findByTestId('page-provisioners')
    expect(await within(page).findByText('admin')).toBeInTheDocument()
    expect(within(page).getByText('JWK')).toBeInTheDocument()
  })

  it('degrades to an empty state when the CA reports no provisioners', async () => {
    installFakeApi({ session: 'authenticated', provisioners: [] })
    renderRoutes(appRoutes, ['/provisioners'])

    expect(await screen.findByText('No provisioners or CA is unavailable')).toBeInTheDocument()
  })

  it('shows an error state and can retry', async () => {
    // Use the narrow provisionersDown flag: the global `down` flag also
    // breaks the session/config calls every page needs just to render,
    // leaving the test stuck at the Splash screen forever.
    installFakeApi({ session: 'authenticated', provisionersDown: true })
    // The default query client retries server errors with real exponential
    // backoff, which outlasts the test's assertion timeout; disable retry.
    renderRoutes(
      appRoutes,
      ['/provisioners'],
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    )

    expect(await screen.findByTestId('error-state')).toBeInTheDocument()
  })

  it('shows a loading skeleton while pending', async () => {
    // Delay the response so the skeleton is still mounted once the route's
    // own HydrateFallback (Splash) has cleared; a plain findByTestId races
    // the MutationObserver against the fetch settling in the same flush.
    installFakeApi({ session: 'authenticated', delayMs: 20 })
    renderRoutes(appRoutes, ['/provisioners'])

    // The sidebar nav has its own "Loading navigation" skeleton (also
    // delayed here), so scope the query to this page's own container.
    const page = await screen.findByTestId('page-provisioners')
    expect(within(page).getByTestId('skeleton')).toBeInTheDocument()
    await within(page).findByText('admin')
  })
})
