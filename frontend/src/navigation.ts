import type { AdminIconName, AppIconName } from './components/icons'

export type Role = 'viewer' | 'manager' | 'admin'
export type Feature = 'acme'

export interface NavLeaf<Icon extends string = never> {
  label: string
  to: string
  requiredRole: Role
  feature?: Feature
  icon?: Icon
  end?: boolean
}

export interface NavSection<Icon extends string> {
  title: string
  requiredRole: Role
  group?: { label: string; icon: Icon }
  items: NavLeaf<Icon>[]
}

export const appNavigation: NavSection<AppIconName>[] = [
  {
    title: 'Home',
    requiredRole: 'viewer',
    items: [
      { label: 'Dashboard', to: '/', requiredRole: 'viewer', icon: 'home', end: true },
      { label: 'All Certificates', to: '/certificates', requiredRole: 'viewer', icon: 'certificates' },
    ],
  },
  {
    title: 'Local CA',
    requiredRole: 'manager',
    group: { label: 'Certificates', icon: 'local' },
    items: [
      { label: 'Issue', to: '/issue', requiredRole: 'manager' },
      { label: 'Import', to: '/import', requiredRole: 'manager' },
    ],
  },
  {
    title: 'Certificate Authority',
    requiredRole: 'viewer',
    group: { label: 'CA Management', icon: 'authority' },
    items: [
      { label: 'Provisioners', to: '/provisioners', requiredRole: 'viewer' },
      { label: 'Operation History', to: '/history', requiredRole: 'viewer' },
    ],
  },
  {
    title: "Let's Encrypt",
    requiredRole: 'manager',
    group: { label: "Let's Encrypt", icon: 'acme' },
    items: [
      { label: 'LE Certificates', to: '/le', requiredRole: 'manager', feature: 'acme', end: true },
      { label: 'Issue', to: '/le/issue', requiredRole: 'manager', feature: 'acme' },
      { label: 'LE Settings', to: '/le/settings', requiredRole: 'manager', feature: 'acme' },
      { label: 'LE Logs', to: '/le/logs', requiredRole: 'manager', feature: 'acme' },
    ],
  },
  {
    title: 'Account',
    requiredRole: 'viewer',
    items: [
      { label: 'Profile', to: '/profile', requiredRole: 'viewer', icon: 'profile' },
      { label: 'Admin Panel', to: '/admin', requiredRole: 'admin', icon: 'authority' },
    ],
  },
]

export const adminNavigation: NavSection<AdminIconName>[] = [
  {
    title: 'Overview',
    requiredRole: 'admin',
    items: [
      { label: 'Dashboard', to: '/admin', requiredRole: 'admin', icon: 'overview', end: true },
      { label: 'CA Activity', to: '/admin/activity', requiredRole: 'admin', icon: 'activity' },
    ],
  },
  {
    title: 'Users',
    requiredRole: 'admin',
    items: [
      { label: 'All Users', to: '/admin/users', requiredRole: 'admin', icon: 'users' },
      { label: 'Temporary Users', to: '/admin/users-temp', requiredRole: 'admin', icon: 'temporary' },
    ],
  },
  {
    title: 'Security',
    requiredRole: 'admin',
    items: [{ label: 'Security Log', to: '/admin/security', requiredRole: 'admin', icon: 'security' }],
  },
  {
    title: 'Server',
    requiredRole: 'admin',
    items: [
      { label: 'Server Console', to: '/admin/console', requiredRole: 'admin', icon: 'console' },
      { label: 'About', to: '/admin/about', requiredRole: 'admin', icon: 'about' },
      { label: 'CA Integrity', to: '/admin/integrity', requiredRole: 'admin', icon: 'integrity' },
      { label: 'Backup', to: '/admin/backup', requiredRole: 'admin', icon: 'backup' },
      { label: 'Notifications', to: '/admin/notifications', requiredRole: 'admin', icon: 'notifications' },
    ],
  },
]

export type RoleLevels = Record<string, number>

export function roleAllows(have: string | undefined, need: Role, levels: RoleLevels): boolean {
  if (!have) return false
  const haveLevel = levels[have]
  const needLevel = levels[need]
  return haveLevel !== undefined && needLevel !== undefined && haveLevel >= needLevel
}

export interface NavFilter {
  role: string | undefined
  levels: RoleLevels
  features: Record<Feature, boolean>
}

// Presentation only: the server enforces every role on every operation. Hiding
// an item is a courtesy, so a role the config does not know hides everything.
export function visibleSections<Icon extends string>(
  sections: readonly NavSection<Icon>[],
  { role, levels, features }: NavFilter,
): NavSection<Icon>[] {
  return sections
    .filter((section) => roleAllows(role, section.requiredRole, levels))
    .map((section) => ({
      ...section,
      items: section.items.filter(
        (item) => roleAllows(role, item.requiredRole, levels) && (!item.feature || features[item.feature]),
      ),
    }))
    .filter((section) => section.items.length > 0)
}
