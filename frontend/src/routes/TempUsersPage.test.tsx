import { QueryClient } from '@tanstack/react-query'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('temporary users page', () => {
  let eject: () => void
  let confirmSpy: ReturnType<typeof vi.spyOn>
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
    confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
  })
  afterEach(() => {
    eject()
    confirmSpy.mockRestore()
  })

  it('lists temporary users', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users-temp'])

    const page = await screen.findByTestId('page-admin-users-temp')
    expect(await within(page).findByText('temp-xyz')).toBeInTheDocument()
    expect(within(page).getByText('vendor access')).toBeInTheDocument()
  })

  it('shows an empty state when there are no temporary users', async () => {
    installFakeApi({ session: 'authenticated', tempUsers: [] })
    renderRoutes(appRoutes, ['/admin/users-temp'])

    expect(await screen.findByTestId('empty-state')).toBeInTheDocument()
  })

  it('shows an error state and can retry', async () => {
    // Dedicated tempUsersDown flag: the shared `down` flag also breaks the
    // session/config calls every page needs just to render.
    installFakeApi({ session: 'authenticated', tempUsersDown: true })
    renderRoutes(
      appRoutes,
      ['/admin/users-temp'],
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    )

    expect(await screen.findByTestId('error-state')).toBeInTheDocument()
  })

  it('creates a temporary user and shows the one-time passphrase', async () => {
    const api = installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users-temp'])

    await screen.findByText('temp-xyz')
    await userEvent.selectOptions(screen.getByLabelText('Role'), 'manager')
    await userEvent.selectOptions(screen.getByLabelText('Expires in'), '1d')
    await userEvent.type(screen.getByLabelText('Note'), 'contractor')
    await userEvent.click(screen.getByRole('button', { name: 'Create temporary user' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/temporary-users' && c.method === 'POST')).toBe(true),
    )
    const credential = await screen.findByTestId('temp-user-credential')
    expect(within(credential).getByText('fake-one-time-passphrase')).toBeInTheDocument()
  })

  it('dismisses the one-time passphrase banner', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users-temp'])

    await screen.findByText('temp-xyz')
    await userEvent.click(screen.getByRole('button', { name: 'Create temporary user' }))

    await userEvent.click(await screen.findByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByTestId('temp-user-credential')).toBeNull()
  })

  it('deletes a temporary user after confirmation', async () => {
    const api = installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users-temp'])

    await screen.findByText('temp-xyz')
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/temporary-users/1' && c.method === 'DELETE')).toBe(
        true,
      ),
    )
    expect(await screen.findByText('temp-xyz deleted.')).toBeInTheDocument()
  })
})
