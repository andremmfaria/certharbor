import type { ComponentType } from 'react'
import { EmptyState } from '../components/EmptyState'
import { testIdFor, type RouteEntry } from './routeList'

// Every leaf is a placeholder until its domain phase lands. The page owns its
// test id, which the smoke tests look for.
export function placeholderFor(route: Pick<RouteEntry, 'name' | 'title'>): ComponentType {
  function Placeholder() {
    return (
      <section data-testid={testIdFor(route)}>
        <div className="page-header">
          <div>
            <div className="breadcrumb">{route.name}</div>
            <h1>{route.title}</h1>
          </div>
        </div>
        <EmptyState title="Nothing here yet">This page has not been built.</EmptyState>
      </section>
    )
  }
  Placeholder.displayName = `Placeholder(${route.name})`
  return Placeholder
}
