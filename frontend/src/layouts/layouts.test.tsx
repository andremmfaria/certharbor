import { screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installClient } from '../api/client'
import { appRoutes } from '../routes/appRoutes'
import { installFakeApi } from '../test/fakeApi'
import { renderRoutes } from '../test/render'

describe('navigation by role', () => {
  let eject: () => void
  beforeEach(() => {
    eject = installClient({ clearCache: vi.fn(), redirectToLogin: vi.fn() })
  })
  afterEach(() => {
    eject()
    document.body.classList.remove('admin-mode')
  })

  async function sidebar() {
    return within(await screen.findByRole('complementary'))
  }

  it('shows a viewer neither the manager groups nor the admin link', async () => {
    installFakeApi({ session: 'authenticated', user: { id: 2, username: 'bob', role: 'viewer' } })
    renderRoutes(appRoutes, ['/'])

    const nav = await sidebar()
    expect(await nav.findByRole('link', { name: 'All Certificates' })).toBeInTheDocument()
    expect(nav.queryByText('Issue')).toBeNull()
    expect(nav.queryByText('Admin Panel')).toBeNull()
    expect(nav.queryByText("Let's Encrypt")).toBeNull()
  })

  it('shows a manager the Local CA and Let’s Encrypt groups but not the admin link', async () => {
    installFakeApi({ session: 'authenticated', user: { id: 3, username: 'mia', role: 'manager' } })
    renderRoutes(appRoutes, ['/'])

    const nav = await sidebar()
    expect(await nav.findByText('Local CA')).toBeInTheDocument()
    expect(nav.getByText("Let's Encrypt", { selector: '.nav-section' })).toBeInTheDocument()
    expect(nav.queryByText('Admin Panel')).toBeNull()
  })

  it('shows an admin the admin link, and the admin layout puts the body in admin mode', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/'])

    expect(await (await sidebar()).findByRole('link', { name: 'Admin Panel' })).toHaveAttribute(
      'href',
      '/admin',
    )

    eject()
    document.body.className = ''
  })

  it('renders the admin layout with its own navigation', async () => {
    installFakeApi({ session: 'authenticated' })
    renderRoutes(appRoutes, ['/admin/users'])

    expect(await screen.findByTestId('page-admin-users')).toBeInTheDocument()
    expect(document.body).toHaveClass('admin-mode')
    expect(screen.getAllByRole('link', { name: /Temporary Users/ }).length).toBeGreaterThan(0)
  })
})
