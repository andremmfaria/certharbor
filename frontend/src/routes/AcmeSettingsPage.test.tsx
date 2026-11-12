import { QueryClient } from '@tanstack/react-query'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('acme settings page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('renders the stored settings', async () => {
    installFakeApi({
      session: 'authenticated',
      user: { id: 3, username: 'mgr', role: 'manager' },
      acmeSettings: {
        email: 'acme@example.com',
        provider: 'cloudflare',
        cfZoneId: 'stored-zone',
        r53KeyId: '',
        r53Region: '',
      },
    })
    renderRoutes(appRoutes, ['/le/settings'])

    const page = await screen.findByTestId('page-acme-settings')
    expect(await within(page).findByLabelText('Account email')).toHaveValue('acme@example.com')
    expect(within(page).getByLabelText('Challenge provider')).toHaveValue('cloudflare')
    expect(within(page).getByLabelText('Cloudflare zone ID')).toHaveValue('stored-zone')
    // writeOnly on the server (5.8): never pre-filled from GET.
    expect(within(page).getByLabelText('Cloudflare API token')).toHaveValue('')
  })

  it('shows a loading skeleton while pending', async () => {
    installFakeApi({ session: 'authenticated', delayMs: 20 })
    renderRoutes(appRoutes, ['/le/settings'])

    const page = await screen.findByTestId('page-acme-settings')
    expect(within(page).getByTestId('skeleton')).toBeInTheDocument()
    await within(page).findByLabelText('Account email')
  })

  it('shows an error state and can retry', async () => {
    // Use the narrow acmeSettingsDown flag: the global `down` flag also
    // breaks the session/config calls every page needs just to render,
    // leaving the test stuck at the Splash screen forever.
    installFakeApi({ session: 'authenticated', acmeSettingsDown: true })
    // The default query client retries server errors with real exponential
    // backoff, which outlasts the test's assertion timeout; disable retry.
    renderRoutes(
      appRoutes,
      ['/le/settings'],
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    )

    expect(await screen.findByTestId('error-state')).toBeInTheDocument()
  })

  it('submits explicit field updates', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      acmeSettings: {
        email: 'acme@example.com',
        provider: 'http01',
        cfZoneId: '',
        r53KeyId: '',
        r53Region: '',
      },
    })
    renderRoutes(appRoutes, ['/le/settings'])

    await screen.findByLabelText('Account email')
    await userEvent.clear(screen.getByLabelText('Account email'))
    await userEvent.type(screen.getByLabelText('Account email'), 'new@example.com')
    await userEvent.selectOptions(screen.getByLabelText('Challenge provider'), 'cloudflare')
    await userEvent.type(screen.getByLabelText('Cloudflare zone ID'), 'new-zone')
    await userEvent.click(screen.getByRole('button', { name: 'Save settings' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/acme/settings' && c.method === 'PATCH')).toBe(true),
    )
    const patched = api.calls.find((c) => c.path === '/api/v1/acme/settings' && c.method === 'PATCH')
    expect(patched?.body).toMatchObject({
      email: 'new@example.com',
      provider: 'cloudflare',
      cfZoneId: 'new-zone',
    })
    expect(await screen.findByText('ACME settings saved.')).toBeInTheDocument()
  })

  // Mirrors handlers.TestApplyACMESettingsPatch_OmittedSecretsUnchanged at
  // the UI level: saving the form without touching either secret field
  // must not send cfToken/r53SecretKey at all, so the server's merge-style
  // PATCH leaves the stored secrets untouched instead of clearing them.
  it('omits untouched secret fields from the PATCH body', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      acmeSettings: {
        email: 'acme@example.com',
        provider: 'cloudflare',
        cfZoneId: 'stored-zone',
        r53KeyId: '',
        r53Region: '',
      },
    })
    renderRoutes(appRoutes, ['/le/settings'])

    await screen.findByLabelText('Account email')
    await userEvent.clear(screen.getByLabelText('Account email'))
    await userEvent.type(screen.getByLabelText('Account email'), 'updated@example.com')
    await userEvent.click(screen.getByRole('button', { name: 'Save settings' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/acme/settings' && c.method === 'PATCH')).toBe(true),
    )
    const patched = api.calls.find((c) => c.path === '/api/v1/acme/settings' && c.method === 'PATCH')
    const body = patched?.body as Record<string, unknown>
    expect(body.email).toBe('updated@example.com')
    expect(body).not.toHaveProperty('cfToken')
    expect(body).not.toHaveProperty('r53SecretKey')
  })

  it('submits a typed secret so it actually replaces the stored one', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      acmeSettings: {
        email: 'acme@example.com',
        provider: 'cloudflare',
        cfZoneId: 'stored-zone',
        r53KeyId: '',
        r53Region: '',
      },
    })
    renderRoutes(appRoutes, ['/le/settings'])

    await screen.findByLabelText('Account email')
    await userEvent.type(screen.getByLabelText('Cloudflare API token'), 'new-cf-token')
    await userEvent.click(screen.getByRole('button', { name: 'Save settings' }))

    await waitFor(() =>
      expect(api.calls.some((c) => c.path === '/api/v1/acme/settings' && c.method === 'PATCH')).toBe(true),
    )
    const patched = api.calls.find((c) => c.path === '/api/v1/acme/settings' && c.method === 'PATCH')
    expect(patched?.body).toMatchObject({ cfToken: 'new-cf-token' })
  })

  it('shows a toast and field errors when the save fails', async () => {
    const api = installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/le/settings'])

    await screen.findByLabelText('Account email')
    api.state.acmeSettingsDown = true
    await userEvent.click(screen.getByRole('button', { name: 'Save settings' }))

    expect(await screen.findByText('ACME settings unavailable')).toBeInTheDocument()
  })
})
