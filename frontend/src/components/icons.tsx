import type { ReactNode } from 'react'

export type AppIconName = 'home' | 'certificates' | 'local' | 'authority' | 'acme' | 'profile' | 'signout'
export type AdminIconName =
  | 'overview'
  | 'activity'
  | 'users'
  | 'temporary'
  | 'security'
  | 'console'
  | 'about'
  | 'integrity'
  | 'backup'
  | 'notifications'
  | 'back'
  | 'signout'

const app: Record<AppIconName, ReactNode> = {
  home: (
    <>
      <path d="M2 6l5-4 5 4v6a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1z" />
      <path d="M6 13V9h2v4" />
    </>
  ),
  certificates: (
    <>
      <rect x="2" y="1" width="10" height="12" rx="2" />
      <line x1="4" y1="5" x2="10" y2="5" />
      <line x1="4" y1="8" x2="8" y2="8" />
    </>
  ),
  local: (
    <>
      <circle cx="7" cy="7" r="5" />
      <line x1="7" y1="4" x2="7" y2="10" />
      <line x1="4" y1="7" x2="10" y2="7" />
    </>
  ),
  authority: (
    <>
      <circle cx="7" cy="7" r="2" />
      <path d="M7 1v2M7 11v2M1 7h2M11 7h2M3.2 3.2l1.4 1.4M9.4 9.4l1.4 1.4M3.2 10.8l1.4-1.4M9.4 4.6l1.4-1.4" />
    </>
  ),
  acme: <path d="M7 1l1.5 4.5H13L9.5 8l1.5 4.5L7 10 3 12.5 4.5 8 1 5.5h4.5Z" />,
  profile: (
    <>
      <circle cx="7" cy="5" r="3" />
      <path d="M2 13c0-2.5 2-4.5 5-4.5s5 2 5 4.5" />
    </>
  ),
  signout: <path d="M5 2H3a1 1 0 0 0-1 1v8a1 1 0 0 0 1 1h2M9 10l3-3-3-3M12 7H6" />,
}

const admin: Record<AdminIconName, ReactNode> = {
  overview: (
    <>
      <rect x="3" y="3" width="7" height="7" />
      <rect x="14" y="3" width="7" height="7" />
      <rect x="14" y="14" width="7" height="7" />
      <rect x="3" y="14" width="7" height="7" />
    </>
  ),
  activity: <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />,
  users: (
    <>
      <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" />
      <circle cx="9" cy="7" r="4" />
      <path d="M23 21v-2a4 4 0 0 0-3-3.87" />
      <path d="M16 3.13a4 4 0 0 1 0 7.75" />
    </>
  ),
  temporary: (
    <>
      <circle cx="12" cy="12" r="10" />
      <polyline points="12 6 12 12 16 14" />
    </>
  ),
  security: <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />,
  console: (
    <>
      <polyline points="4 17 10 11 4 5" />
      <line x1="12" y1="19" x2="20" y2="19" />
    </>
  ),
  about: (
    <>
      <circle cx="12" cy="12" r="10" />
      <line x1="12" y1="16" x2="12" y2="12" />
      <line x1="12" y1="8" x2="12.01" y2="8" />
    </>
  ),
  integrity: (
    <>
      <path d="M20 13c0 5-3.5 8-8 9-4.5-1-8-4-8-9V5l8-3 8 3v8Z" />
      <path d="m9 12 2 2 4-5" />
    </>
  ),
  backup: (
    <>
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
      <polyline points="7 10 12 15 17 10" />
      <line x1="12" y1="15" x2="12" y2="3" />
    </>
  ),
  notifications: (
    <>
      <path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 7h18s-3 0-3-7" />
      <path d="M13.73 21a2 2 0 0 1-3.46 0" />
    </>
  ),
  back: <polyline points="15 18 9 12 15 6" />,
  signout: (
    <>
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
      <polyline points="16 17 21 12 16 7" />
      <line x1="21" y1="12" x2="9" y2="12" />
    </>
  ),
}

export function AppIcon({ name }: { name: AppIconName }) {
  return (
    <svg
      className="nav-icon"
      viewBox="0 0 14 14"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {app[name]}
    </svg>
  )
}

export function AdminIcon({ name, size = 16 }: { name: AdminIconName; size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {admin[name]}
    </svg>
  )
}
