import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('issue page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('seeds the picker from the default template and issues on submit', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
    })
    renderRoutes(appRoutes, ['/issue'])

    expect(await screen.findByText('Server')).toBeInTheDocument()
    await userEvent.type(screen.getByLabelText('Name'), 'my-cert')
    await userEvent.type(screen.getByLabelText('Domain'), 'my.example.test')
    await userEvent.click(screen.getByRole('button', { name: 'Issue Certificate' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/certificates' && c.method === 'POST')).toBe(true),
    )
    const created = api.calls.find((c) => c.path === '/api/v1/certificates' && c.method === 'POST')
    expect(created?.body).toMatchObject({ name: 'my-cert', domain: 'my.example.test', template: 'server' })
  })

  it('is refused to a viewer', async () => {
    installFakeApi({ session: 'authenticated', user: { id: 2, username: 'bob', role: 'viewer' } })
    renderRoutes(appRoutes, ['/issue'])

    expect(await screen.findByText('Access denied')).toBeInTheDocument()
  })
})
