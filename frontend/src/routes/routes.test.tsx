import { screen } from '@testing-library/react'
import { matchPath } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { adminNavigation, appNavigation } from '../navigation'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'
import { appRoutes } from './appRoutes'
import { routeList, testIdFor } from './routeList'

describe('route table', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => eject())

  it('has no duplicate paths', () => {
    const paths = routeList.map((r) => r.path)
    expect(new Set(paths).size).toBe(paths.length)
  })

  it('covers every navigation item', () => {
    const targets = [...appNavigation, ...adminNavigation].flatMap((s) => s.items.map((i) => i.to))
    for (const to of targets) {
      expect(
        routeList.some((r) => matchPath(r.path, to)),
        `${to} has no route`,
      ).toBe(true)
    }
  })

  it.each(routeList.map((r) => [r.smokePath, r] as const))(
    'renders %s with its page test id',
    async (path, route) => {
      installFakeApi({ session: route.layout === 'auth' ? 'anonymous' : 'authenticated' })
      renderRoutes(appRoutes, [path])

      expect(await screen.findByTestId(testIdFor(route))).toBeInTheDocument()
    },
  )

  it('renders an unknown path as not found inside the shell', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/no/such/page'])

    expect(await screen.findByTestId('page-not-found')).toBeInTheDocument()
  })

  it('refuses an admin page to a non-admin role', async () => {
    installFakeApi({ session: 'authenticated', user: { id: 2, username: 'bob', role: 'viewer' } })
    renderRoutes(appRoutes, ['/admin/users'])

    expect(await screen.findByText('Access denied')).toBeInTheDocument()
    expect(screen.queryByTestId('page-admin-users')).toBeNull()
  })

  it('refuses a manager page to a viewer', async () => {
    installFakeApi({ session: 'authenticated', user: { id: 2, username: 'bob', role: 'viewer' } })
    renderRoutes(appRoutes, ['/issue'])

    expect(await screen.findByText('Access denied')).toBeInTheDocument()
  })
})
