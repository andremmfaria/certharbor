import type { ComponentType } from 'react'
import { ActivityPage } from './ActivityPage'
import { placeholderFor } from './Placeholder'
import { routeList } from './routeList'
import { SecurityPage } from './SecurityPage'

// The admin leaves built for real so far; everything else is still a
// placeholder until its own phase lands (mirrors appRoutes.tsx's BUILT_PAGES).
const BUILT_ADMIN_PAGES: Record<string, ComponentType> = {
  'admin-security': SecurityPage,
  'admin-activity': ActivityPage,
}

// One lazy chunk for every admin leaf (D12.7).
export const adminPages: Record<string, ComponentType> = Object.fromEntries(
  routeList
    .filter((r) => r.layout === 'admin')
    .map((r) => [r.name, BUILT_ADMIN_PAGES[r.name] ?? placeholderFor(r)]),
)
