import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('console page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('lists commands and never shows a database DSN', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/console'])

    expect(await screen.findByText('Select a command…')).toBeInTheDocument()
    expect(screen.queryByText(/postgres:\/\//)).toBeNull()
  })

  it('runs the selected command and shows its output', async () => {
    const api = installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/console'])

    await screen.findByLabelText('Command')
    await userEvent.selectOptions(screen.getByLabelText('Command'), 'pg_isready')
    await userEvent.click(screen.getByRole('button', { name: 'Run' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/admin/console/run' && c.method === 'POST')).toBe(true),
    )
    expect(await screen.findByTestId('console-result')).toBeInTheDocument()
    expect(screen.getByText('pg_isready: ok')).toBeInTheDocument()
    expect(screen.getByText('success')).toBeInTheDocument()
  })

  it('surfaces a run failure as a toast', async () => {
    installFakeApi({ session: 'authenticated', adminConsoleRunFails: true })
    renderRoutes(appRoutes, ['/admin/console'])

    await userEvent.selectOptions(await screen.findByLabelText('Command'), 'pg_isready')
    await userEvent.click(screen.getByRole('button', { name: 'Run' }))

    expect(
      await screen.findByText('unknown command_id: only allowlisted commands may be run'),
    ).toBeInTheDocument()
  })
})
