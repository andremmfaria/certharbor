import { screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'

describe('certificate detail page', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('renders the real certificate detail fields', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/certificates/1'])

    expect(await screen.findByTestId('page-certificate-detail')).toBeInTheDocument()
    expect(await screen.findByText('CN=example.test')).toBeInTheDocument()
    expect(screen.getByText('sha256:fake')).toBeInTheDocument()
    expect(screen.getByText('chain')).toBeInTheDocument()
  })

  it('renders an error state for a certificate that does not exist', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/certificates/999'])

    expect(await screen.findByTestId('error-state')).toBeInTheDocument()
  })

  it('offers CRT and KEY downloads', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/certificates/1'])

    expect(await screen.findByRole('button', { name: 'Download CRT' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Download KEY' })).toBeInTheDocument()
  })
})
