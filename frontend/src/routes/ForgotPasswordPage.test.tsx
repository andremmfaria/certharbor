import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('forgot password view', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  async function submit(email: string) {
    await userEvent.type(await screen.findByLabelText('Email or username'), email)
    await userEvent.click(screen.getByRole('button', { name: /send reset link/i }))
  }

  it('shows the same generic message for a known account', async () => {
    const api = installFakeApi()
    renderRoutes(appRoutes, ['/forgot-password'])

    await submit('alice')

    expect(await screen.findByText(/check your email/i)).toBeInTheDocument()
    expect(api.calls.find((c) => c.path === '/api/v1/password-reset/requests')?.body).toEqual({
      email: 'alice',
    })
  })

  it('shows the identical generic message for an unknown account', async () => {
    installFakeApi()
    renderRoutes(appRoutes, ['/forgot-password'])

    await submit('nobody-by-this-name')

    expect(await screen.findByText(/check your email/i)).toBeInTheDocument()
  })

  it('validates an empty field before sending anything', async () => {
    const api = installFakeApi()
    renderRoutes(appRoutes, ['/forgot-password'])

    await userEvent.click(await screen.findByRole('button', { name: /send reset link/i }))

    expect(await screen.findByText('Email or username is required')).toBeInTheDocument()
    expect(api.calls.filter((c) => c.method === 'POST')).toHaveLength(0)
  })

  it('shows a transient loading state while the request is in flight', async () => {
    installFakeApi({ delayMs: 50 })
    renderRoutes(appRoutes, ['/forgot-password'])

    await userEvent.type(await screen.findByLabelText('Email or username'), 'alice')
    await userEvent.click(screen.getByRole('button', { name: /send reset link/i }))

    expect(await screen.findByRole('button', { name: /sending/i })).toBeDisabled()
    expect(await screen.findByText(/check your email/i)).toBeInTheDocument()
  })

  it('shows a retryable error on a genuine server failure, not the generic message', async () => {
    installFakeApi({ requestPasswordResetDown: true })
    renderRoutes(appRoutes, ['/forgot-password'])

    await submit('alice')

    expect(await screen.findByTestId('forgot-password-error')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText(/check your email/i)).toBeNull())
  })
})
