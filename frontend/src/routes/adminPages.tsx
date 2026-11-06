import type { ComponentType } from 'react'
import { placeholderFor } from './Placeholder'
import { routeList } from './routeList'

// One lazy chunk for every admin leaf (D12.7).
export const adminPages: Record<string, ComponentType> = Object.fromEntries(
  routeList.filter((r) => r.layout === 'admin').map((r) => [r.name, placeholderFor(r)]),
)
