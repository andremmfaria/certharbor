import { QueryClient } from '@tanstack/react-query'
import { screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('integrity page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('lists integrity checks', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/integrity'])

    expect(await screen.findByText('root certificate')).toBeInTheDocument()
    expect(screen.getByText('valid')).toBeInTheDocument()
  })

  it('shows an empty state when there are no checks', async () => {
    installFakeApi({ session: 'authenticated', adminIntegrity: [] })
    renderRoutes(appRoutes, ['/admin/integrity'])

    expect(await screen.findByTestId('empty-state')).toBeInTheDocument()
  })

  it('shows an error state and can retry', async () => {
    installFakeApi({ session: 'authenticated', adminIntegrityDown: true })
    renderRoutes(
      appRoutes,
      ['/admin/integrity'],
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    )

    expect(await screen.findByTestId('error-state')).toBeInTheDocument()
  })
})
