import { QueryClient } from '@tanstack/react-query'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('profile page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('renders the signed-in user', async () => {
    installFakeApi({
      session: 'authenticated',
      user: {
        id: 1,
        username: 'alice',
        role: 'admin',
        displayName: 'Alice Admin',
        email: 'alice@example.com',
        theme: 'dark',
      },
    })
    renderRoutes(appRoutes, ['/profile'])

    const page = await screen.findByTestId('page-profile')
    expect(await within(page).findByLabelText('Display name')).toHaveValue('Alice Admin')
    expect(within(page).getByLabelText('Email')).toHaveValue('alice@example.com')
    expect(within(page).getByLabelText('Dark theme')).toBeChecked()
  })

  it('shows a loading skeleton while pending', async () => {
    installFakeApi({ session: 'authenticated', delayMs: 20 })
    renderRoutes(appRoutes, ['/profile'])

    const page = await screen.findByTestId('page-profile')
    expect(within(page).getByTestId('skeleton')).toBeInTheDocument()
    await within(page).findByLabelText('Display name')
  })

  it('shows an error state and can retry', async () => {
    installFakeApi({ session: 'authenticated', profileDown: true })
    renderRoutes(appRoutes, ['/profile'], new QueryClient({ defaultOptions: { queries: { retry: false } } }))

    expect(await screen.findByTestId('error-state')).toBeInTheDocument()
  })

  it('saves display name and email', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      user: {
        id: 1,
        username: 'alice',
        role: 'admin',
        displayName: 'Alice Admin',
        email: 'alice@example.com',
        theme: 'dark',
      },
    })
    renderRoutes(appRoutes, ['/profile'])

    await screen.findByLabelText('Display name')
    await userEvent.clear(screen.getByLabelText('Display name'))
    await userEvent.type(screen.getByLabelText('Display name'), 'Alice Updated')
    await userEvent.click(screen.getByRole('button', { name: 'Save details' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/profile' && c.method === 'PATCH')).toBe(true),
    )
    const patched = api.calls.find((c) => c.path === '/api/v1/profile' && c.method === 'PATCH')
    expect(patched?.body).toMatchObject({ displayName: 'Alice Updated' })
    expect(await screen.findByText('Profile saved.')).toBeInTheDocument()
  })

  it('toggles the theme preference', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      user: {
        id: 1,
        username: 'alice',
        role: 'admin',
        displayName: 'Alice Admin',
        email: 'alice@example.com',
        theme: 'dark',
      },
    })
    renderRoutes(appRoutes, ['/profile'])

    await screen.findByLabelText('Dark theme')
    await userEvent.click(screen.getByLabelText('Dark theme'))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/profile/preferences' && c.method === 'PATCH')).toBe(
        true,
      ),
    )
    const patched = api.calls.find((c) => c.path === '/api/v1/profile/preferences' && c.method === 'PATCH')
    expect(patched?.body).toMatchObject({ theme: 'light' })
  })

  it('changes the password on correct current password', async () => {
    const api = installFakeApi({ session: 'authenticated', profilePassword: 'old-pass' })
    renderRoutes(appRoutes, ['/profile'])

    await screen.findByLabelText('Current password')
    await userEvent.type(screen.getByLabelText('Current password'), 'old-pass')
    await userEvent.type(screen.getByLabelText('New password'), 'new-pass')
    await userEvent.click(screen.getByRole('button', { name: 'Change password' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/profile/password' && c.method === 'POST')).toBe(true),
    )
    expect(await screen.findByText('Password changed.')).toBeInTheDocument()
  })

  it('surfaces an incorrect-current-password error distinctly from validation', async () => {
    installFakeApi({ session: 'authenticated', profilePassword: 'old-pass' })
    renderRoutes(appRoutes, ['/profile'])

    await screen.findByLabelText('Current password')
    await userEvent.type(screen.getByLabelText('Current password'), 'wrong-pass')
    await userEvent.type(screen.getByLabelText('New password'), 'new-pass')
    await userEvent.click(screen.getByRole('button', { name: 'Change password' }))

    expect(await screen.findByText('Current password is incorrect.')).toBeInTheDocument()
    // Not the generic toast path used for validation/other failures.
    expect(screen.queryByText('Password change failed.')).not.toBeInTheDocument()
  })
})
