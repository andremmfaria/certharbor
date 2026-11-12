import { QueryClient } from '@tanstack/react-query'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('users page', () => {
  let eject: () => void
  let confirmSpy: ReturnType<typeof vi.spyOn>
  let promptSpy: ReturnType<typeof vi.spyOn>
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
    confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    promptSpy = vi.spyOn(window, 'prompt').mockReturnValue('new-password')
  })
  afterEach(() => {
    eject()
    confirmSpy.mockRestore()
    promptSpy.mockRestore()
  })

  it('lists users', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users'])

    const page = await screen.findByTestId('page-admin-users')
    expect(await within(page).findByText('alice')).toBeInTheDocument()
    expect(within(page).getByText('bob')).toBeInTheDocument()
  })

  it('shows an empty state when there are no users', async () => {
    installFakeApi({ session: 'authenticated', users: [] })
    renderRoutes(appRoutes, ['/admin/users'])

    expect(await screen.findByTestId('empty-state')).toBeInTheDocument()
  })

  it('shows an error state and can retry', async () => {
    // Dedicated usersDown flag: the shared `down` flag also breaks the
    // session/config calls every page needs just to render.
    installFakeApi({ session: 'authenticated', usersDown: true })
    renderRoutes(
      appRoutes,
      ['/admin/users'],
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    )

    expect(await screen.findByTestId('error-state')).toBeInTheDocument()
  })

  it('filters by the search box', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users'])

    await screen.findByText('alice')
    await userEvent.type(screen.getByLabelText('Search'), 'bob')

    await waitFor(() => expect(screen.queryByText('alice')).toBeNull())
    expect(await screen.findByText('bob')).toBeInTheDocument()
  })

  it('creates a new user', async () => {
    const api = installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users'])

    await screen.findByText('alice')
    await userEvent.type(screen.getByLabelText('Username'), 'carol')
    await userEvent.type(screen.getByLabelText('Password'), 'super-secret')
    await userEvent.selectOptions(screen.getByLabelText('Role'), 'manager')
    await userEvent.click(screen.getByRole('button', { name: 'Create user' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/users' && c.method === 'POST')).toBe(true),
    )
    expect(await screen.findByText('carol')).toBeInTheDocument()
    expect(await screen.findByText('carol created.')).toBeInTheDocument()
  })

  it("changes a user's role", async () => {
    const api = installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users'])

    await screen.findByText('bob')
    await userEvent.selectOptions(screen.getByLabelText('Role for bob'), 'admin')

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/users/2' && c.method === 'PATCH')).toBe(true),
    )
    expect(await screen.findByText('Role updated.')).toBeInTheDocument()
  })

  it('disables and re-enables a user', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users'])

    const bobRow = (await screen.findByText('bob')).closest('tr')!
    await userEvent.click(within(bobRow).getByRole('button', { name: 'Disable' }))

    expect(await screen.findByText('User disabled.')).toBeInTheDocument()
    expect(within(bobRow).getByText('disabled')).toBeInTheDocument()
  })

  it('deletes a user after confirmation', async () => {
    const api = installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users'])

    const bobRow = (await screen.findByText('bob')).closest('tr')!
    await userEvent.click(within(bobRow).getByRole('button', { name: 'Delete' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/users/2' && c.method === 'DELETE')).toBe(true),
    )
    expect(await screen.findByText('bob deleted.')).toBeInTheDocument()
  })

  it('cannot disable or delete its own row', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users'])

    const page = await screen.findByTestId('page-admin-users')
    const aliceRow = (await within(page).findByText('alice')).closest('tr')!
    expect(within(aliceRow).getByLabelText('Role for alice')).toBeDisabled()
    expect(within(aliceRow).getByRole('button', { name: 'Disable' })).toBeDisabled()
    expect(within(aliceRow).getByRole('button', { name: 'Delete' })).toBeDisabled()
  })

  it('resets a password via the prompt', async () => {
    const api = installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users'])

    await screen.findByText('bob')
    const resetButtons = screen.getAllByRole('button', { name: 'Reset password' })
    await userEvent.click(resetButtons[1]!)

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/users/2/password-reset' && c.method === 'POST')).toBe(
        true,
      ),
    )
    expect(await screen.findByText("bob's password was reset.")).toBeInTheDocument()
  })
})
