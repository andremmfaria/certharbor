import { screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { RootShell } from '../routes/RootShell'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'

const buildSha = vi.hoisted(() => vi.fn<() => string>())
vi.mock('../contractSha', () => ({ buildContractSha: buildSha }))

const routes = [{ Component: RootShell, children: [{ index: true, element: <div data-testid="page" /> }] }]

async function renderShell(builtWith: string, servedBy: string) {
  buildSha.mockReturnValue(builtWith)
  const eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  installFakeApi({ contractSha: servedBy })
  renderRoutes(routes, ['/'])
  await screen.findByTestId('page')
  return eject
}

describe('contract skew banner', () => {
  let eject: (() => void) | undefined
  afterEach(() => eject?.())

  it('appears, without blocking the shell, when the server reports a different contract', async () => {
    eject = await renderShell('built-sha', 'server-sha')

    const banner = await screen.findByTestId('skew-banner')
    expect(banner).toHaveAttribute('role', 'status')
    expect(screen.getByTestId('page')).toBeInTheDocument()
  })

  it('is absent when the digests match', async () => {
    eject = await renderShell('same', 'same')

    await waitFor(() => expect(screen.getByTestId('page')).toBeInTheDocument())
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(screen.queryByTestId('skew-banner')).toBeNull()
  })

  it('is absent when the build carries no digest, as under vite dev', async () => {
    eject = await renderShell('', 'server-sha')

    await waitFor(() => expect(screen.getByTestId('page')).toBeInTheDocument())
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(screen.queryByTestId('skew-banner')).toBeNull()
  })
})
