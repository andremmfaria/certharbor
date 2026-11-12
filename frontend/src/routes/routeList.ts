import type { Role } from '../navigation'

export type LayoutName = 'auth' | 'app' | 'admin'

export interface RouteEntry {
  path: string
  name: string
  title: string
  layout: LayoutName
  requiredRole: Role | undefined
  // A concrete address for a path with parameters, for smoke tests that load every route.
  smokePath: string
}

function entry(
  layout: LayoutName,
  path: string,
  name: string,
  title: string,
  requiredRole?: Role,
  smokePath: string = path,
): RouteEntry {
  return { path, name, title, layout, requiredRole, smokePath }
}

// The committed route list. The router is built from it and the smoke tests
// are table-driven from it, so a route added here without coverage fails and
// a route added to the router without an entry here cannot exist.
export const routeList: readonly RouteEntry[] = [
  entry('auth', '/login', 'login', 'Sign in'),
  entry('auth', '/forgot-password', 'forgot-password', 'Forgot password'),
  entry('auth', '/reset-password', 'reset-password', 'Reset password'),

  entry('app', '/', 'dashboard', 'Dashboard'),
  entry('app', '/dashboard', 'dashboard', 'Dashboard'),
  entry('app', '/certificates', 'certificates', 'Certificates'),
  entry('app', '/certificates/:id', 'certificate-detail', 'Certificate', undefined, '/certificates/1'),
  entry('app', '/issue', 'issue', 'Issue a certificate', 'manager'),
  entry('app', '/import', 'import', 'Import certificates', 'manager'),
  entry('app', '/provisioners', 'provisioners', 'Provisioners'),
  entry('app', '/history', 'history', 'Operation history'),
  entry('app', '/acme', 'acme', "Let's Encrypt certificates", 'manager'),
  entry('app', '/acme/issue', 'acme-issue', "Issue a Let's Encrypt certificate", 'manager'),
  entry('app', '/le/settings', 'acme-settings', "Let's Encrypt settings", 'manager'),
  entry('app', '/le/logs', 'acme-logs', "Let's Encrypt logs", 'manager'),
  entry('app', '/profile', 'profile', 'Profile'),
  entry('app', '/profile/2fa', 'profile-mfa', 'Two-factor authentication'),

  entry('admin', '/admin', 'admin', 'Admin overview', 'admin'),
  entry('admin', '/admin/activity', 'admin-activity', 'CA activity', 'admin'),
  entry('admin', '/admin/users', 'admin-users', 'Users', 'admin'),
  entry('admin', '/admin/users/:id', 'admin-user', 'User', 'admin', '/admin/users/1'),
  entry('admin', '/admin/users-temp', 'admin-users-temp', 'Temporary users', 'admin'),
  entry('admin', '/admin/security', 'admin-security', 'Security log', 'admin'),
  entry('admin', '/admin/console', 'admin-console', 'Server console', 'admin'),
  entry('admin', '/admin/about', 'admin-about', 'About', 'admin'),
  entry('admin', '/admin/integrity', 'admin-integrity', 'CA integrity', 'admin'),
  entry('admin', '/admin/backup', 'admin-backup', 'Backup', 'admin'),
  entry('admin', '/admin/notifications', 'admin-notifications', 'Notifications', 'admin'),
]

export function testIdFor(route: Pick<RouteEntry, 'name'>): string {
  return `page-${route.name}`
}
