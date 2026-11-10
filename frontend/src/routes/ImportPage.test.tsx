import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('import page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('lists candidates and imports one by path, each with its own result', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
    })
    renderRoutes(appRoutes, ['/import'])

    await userEvent.click(await screen.findByRole('button', { name: 'Candidates' }))
    expect(await screen.findByText('orphan')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Import' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/certificates/import/paths')).toBe(true),
    )
    const call = api.calls.find((c) => c.path === '/api/v1/certificates/import/paths')
    expect(call?.body).toEqual([
      { name: 'orphan', domain: 'orphan', certPath: 'orphan.crt', keyPath: 'orphan.key' },
    ])
  })

  it('imports manually by path with both name and domain', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
    })
    renderRoutes(appRoutes, ['/import'])

    await userEvent.click(await screen.findByRole('button', { name: 'Manual' }))
    await userEvent.type(screen.getByLabelText('Name'), 'manual-cert')
    await userEvent.type(screen.getByLabelText('Domain'), 'manual.test')
    await userEvent.type(screen.getByLabelText('Certificate path'), '/certs/manual.crt')
    await userEvent.click(screen.getByRole('button', { name: 'Import' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/certificates/import/paths')).toBe(true),
    )
    const call = api.calls.find((c) => c.path === '/api/v1/certificates/import/paths')
    expect(call?.body).toMatchObject([
      { name: 'manual-cert', domain: 'manual.test', certPath: '/certs/manual.crt' },
    ])
  })

  it('is refused to a viewer', async () => {
    installFakeApi({ session: 'authenticated', user: { id: 2, username: 'bob', role: 'viewer' } })
    renderRoutes(appRoutes, ['/import'])

    expect(await screen.findByText('Access denied')).toBeInTheDocument()
  })
})
