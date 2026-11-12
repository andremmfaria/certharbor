import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('reset password view', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('shows the new password form for a live token', async () => {
    installFakeApi()
    renderRoutes(appRoutes, ['/reset-password?token=valid-reset-token'])

    expect(await screen.findByLabelText('New password')).toBeInTheDocument()
  })

  it('shows an error for an invalid or expired token', async () => {
    installFakeApi()
    renderRoutes(appRoutes, ['/reset-password?token=not-the-right-token'])

    expect(await screen.findByTestId('reset-password-token-error')).toBeInTheDocument()
    expect(screen.queryByLabelText('New password')).toBeNull()
  })

  it('strips the token from the address bar on mount', async () => {
    installFakeApi()
    const replaceStateSpy = vi.spyOn(window.history, 'replaceState')

    renderRoutes(appRoutes, ['/reset-password?token=valid-reset-token'])
    await screen.findByLabelText('New password')

    expect(replaceStateSpy).toHaveBeenCalled()
    const [, , url] = replaceStateSpy.mock.calls[0] as [unknown, string, string]
    expect(String(url)).not.toContain('token=')
    replaceStateSpy.mockRestore()
  })

  it('shows success after submitting a valid new password', async () => {
    const api = installFakeApi()
    renderRoutes(appRoutes, ['/reset-password?token=valid-reset-token'])

    await userEvent.type(await screen.findByLabelText('New password'), 'a-much-better-password')
    await userEvent.click(screen.getByRole('button', { name: /set new password/i }))

    expect(await screen.findByText(/password has been changed/i)).toBeInTheDocument()
    expect(api.calls.find((c) => c.path === '/api/v1/password-reset/confirm')?.body).toEqual({
      token: 'valid-reset-token',
      newPassword: 'a-much-better-password',
    })
  })

  it('shows the server-side validation error for a weak password', async () => {
    const api = installFakeApi()
    renderRoutes(appRoutes, ['/reset-password?token=valid-reset-token'])

    await userEvent.type(await screen.findByLabelText('New password'), 'short')
    await userEvent.click(screen.getByRole('button', { name: /set new password/i }))

    expect(await screen.findByText(/at least 8 characters/i)).toBeInTheDocument()
    await waitFor(() =>
      expect(api.calls.filter((c) => c.path === '/api/v1/password-reset/confirm')).toHaveLength(1),
    )
    expect(screen.queryByText(/password has been changed/i)).toBeNull()
  })
})
