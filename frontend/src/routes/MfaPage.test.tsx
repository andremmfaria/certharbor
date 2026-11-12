import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('mfa page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
    // navigator.clipboard is undefined under jsdom by default.
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } })
  })
  afterEach(() => eject())

  it('enrolls end to end and shows recovery codes once', async () => {
    installFakeApi({
      session: 'authenticated',
      user: { id: 1, username: 'alice', role: 'viewer' },
      mfaEnabled: false,
      mfaConfirmCode: '654321',
      mfaRecoveryCodes: ['AAAA-BBBB-CCCC', 'DDDD-EEEE-FFFF'],
    })
    renderRoutes(appRoutes, ['/profile/2fa'])

    const page = await screen.findByTestId('page-profile-mfa')
    await userEvent.click(await within(page).findByRole('button', { name: 'Enable 2FA' }))

    const enrollment = await within(page).findByTestId('mfa-enrollment')
    expect(within(enrollment).getByTestId('mfa-manual-entry-key')).toHaveTextContent('FAKESECRETFAKESECRET')
    await within(enrollment).findByAltText('Scan this QR code with your authenticator app')

    await userEvent.type(within(enrollment).getByLabelText('Code from your authenticator app'), '654321')
    await userEvent.click(within(enrollment).getByRole('button', { name: 'Confirm' }))

    const codesPanel = await within(page).findByTestId('mfa-recovery-codes')
    expect(within(codesPanel).getByText('AAAA-BBBB-CCCC')).toBeInTheDocument()
    expect(within(codesPanel).getByText('DDDD-EEEE-FFFF')).toBeInTheDocument()

    // Dismissing the one-time view returns to the enabled state; the codes
    // are gone from the DOM and there is no way to bring them back.
    await userEvent.click(within(codesPanel).getByRole('button', { name: "I've saved these codes" }))
    await within(page).findByTestId('mfa-disable')
    expect(screen.queryByTestId('mfa-recovery-codes')).not.toBeInTheDocument()
    expect(screen.queryByText('AAAA-BBBB-CCCC')).not.toBeInTheDocument()
  })

  it('rejects a wrong confirm code and stays on the enrollment step', async () => {
    installFakeApi({
      session: 'authenticated',
      user: { id: 1, username: 'alice', role: 'viewer' },
      mfaConfirmCode: '654321',
    })
    renderRoutes(appRoutes, ['/profile/2fa'])

    const page = await screen.findByTestId('page-profile-mfa')
    await userEvent.click(await within(page).findByRole('button', { name: 'Enable 2FA' }))
    const enrollment = await within(page).findByTestId('mfa-enrollment')

    await userEvent.type(within(enrollment).getByLabelText('Code from your authenticator app'), '000000')
    await userEvent.click(within(enrollment).getByRole('button', { name: 'Confirm' }))

    expect(await within(enrollment).findByTestId('mfa-confirm-error')).toHaveTextContent(
      'That code was not accepted.',
    )
  })

  it('shows a distinct message for a wrong password on disable', async () => {
    installFakeApi({
      session: 'authenticated',
      user: { id: 1, username: 'alice', role: 'viewer' },
      mfaEnabled: true,
      password: 'correct horse',
      mfaConfirmCode: '654321',
    })
    renderRoutes(appRoutes, ['/profile/2fa'])

    const disable = await screen.findByTestId('mfa-disable')
    await userEvent.type(within(disable).getByLabelText('Current password'), 'wrong-password')
    await userEvent.type(within(disable).getByLabelText('Authenticator code'), '654321')
    await userEvent.click(within(disable).getByRole('button', { name: 'Disable 2FA' }))

    expect(await within(disable).findByTestId('mfa-disable-error')).toHaveTextContent(
      'Current password is incorrect.',
    )
  })

  it('shows a distinct message for a wrong code on disable', async () => {
    const api = installFakeApi({
      session: 'authenticated',
      user: { id: 1, username: 'alice', role: 'viewer' },
      mfaEnabled: true,
      password: 'correct horse',
      mfaConfirmCode: '654321',
    })
    renderRoutes(appRoutes, ['/profile/2fa'])

    const disable = await screen.findByTestId('mfa-disable')
    await userEvent.type(within(disable).getByLabelText('Current password'), 'correct horse')
    await userEvent.type(within(disable).getByLabelText('Authenticator code'), '000000')
    await userEvent.click(within(disable).getByRole('button', { name: 'Disable 2FA' }))

    expect(await within(disable).findByTestId('mfa-disable-error')).toHaveTextContent(
      'That authenticator code was not accepted.',
    )

    await waitFor(() => expect(api.calls.some((c) => c.path === '/api/v1/profile/mfa/disable')).toBe(true))
    // Still enabled: the wrong-code attempt must not have disabled MFA.
    expect(api.state.mfaEnabled).toBe(true)
  })
})
