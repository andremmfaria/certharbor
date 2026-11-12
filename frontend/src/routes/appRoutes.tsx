import type { RouteObject } from 'react-router'
import type { ComponentType } from 'react'
import { AuthGate } from '../auth/AuthGate'
import { LoginPage } from '../auth/LoginPage'
import { Splash } from '../components/Splash'
import { AppLayout } from '../layouts/AppLayout'
import { AuthLayout } from '../layouts/AuthLayout'
import { AcmeCertificatesPage } from './AcmeCertificatesPage'
import { AcmeEventsPage } from './AcmeEventsPage'
import { AcmeIssuePage } from './AcmeIssuePage'
import { AcmeSettingsPage } from './AcmeSettingsPage'
import { CertificateDetailPage } from './CertificateDetailPage'
import { CertificatesPage } from './CertificatesPage'
import { DashboardPage } from './DashboardPage'
import { HistoryPage } from './HistoryPage'
import { ImportPage } from './ImportPage'
import { IssuePage } from './IssuePage'
import { NotFoundPage } from './NotFoundPage'
import { placeholderFor } from './Placeholder'
import { ProfilePage } from './ProfilePage'
import { ProvisionersPage } from './ProvisionersPage'
import { RequireRole } from './RequireRole'
import { RootShell } from './RootShell'
import { routeList, type RouteEntry } from './routeList'

function relative(path: string, prefix = ''): string {
  return path.slice(prefix.length).replace(/^\//, '')
}

// The pages built for real so far; everything else is still a placeholder
// until its own phase lands.
const BUILT_PAGES: Record<string, ComponentType> = {
  dashboard: DashboardPage,
  certificates: CertificatesPage,
  'certificate-detail': CertificateDetailPage,
  issue: IssuePage,
  import: ImportPage,
  history: HistoryPage,
  provisioners: ProvisionersPage,
  acme: AcmeCertificatesPage,
  'acme-issue': AcmeIssuePage,
  'acme-settings': AcmeSettingsPage,
  'acme-logs': AcmeEventsPage,
  profile: ProfilePage,
}

function leaf(route: RouteEntry): RouteObject {
  const Page = BUILT_PAGES[route.name] ?? placeholderFor(route)
  if (route.path === '/') return { index: true, Component: Page }
  if (route.requiredRole && route.requiredRole !== 'viewer') {
    return {
      path: relative(route.path),
      element: <RequireRole role={route.requiredRole} />,
      children: [{ index: true, Component: Page }],
    }
  }
  return { path: relative(route.path), Component: Page }
}

function adminLeaf(route: RouteEntry): RouteObject {
  const load = async () => {
    const { adminPages } = await import('./adminPages')
    return { Component: adminPages[route.name] }
  }
  if (route.path === '/admin') return { index: true, lazy: load }
  return { path: relative(route.path, '/admin'), lazy: load }
}

const byLayout = (layout: RouteEntry['layout']) => routeList.filter((r) => r.layout === layout)

const authRoutes: RouteObject[] = byLayout('auth').map((route) =>
  route.name === 'login'
    ? { path: 'login', Component: LoginPage }
    : { path: relative(route.path), Component: placeholderFor(route) },
)

export const appRoutes: RouteObject[] = [
  {
    Component: RootShell,
    HydrateFallback: Splash,
    children: [
      { Component: AuthLayout, children: authRoutes },
      {
        Component: AuthGate,
        children: [
          {
            Component: AppLayout,
            children: [...byLayout('app').map(leaf), { path: '*', Component: NotFoundPage }],
          },
          {
            element: <RequireRole role="admin" standalone />,
            children: [
              {
                path: 'admin',
                lazy: () => import('../layouts/AdminLayout'),
                children: byLayout('admin').map(adminLeaf),
              },
            ],
          },
        ],
      },
    ],
  },
]
