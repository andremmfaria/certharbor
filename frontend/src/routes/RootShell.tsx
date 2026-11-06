import { Outlet } from 'react-router'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { SkewBanner } from '../components/SkewBanner'

export function RootShell() {
  return (
    <ErrorBoundary scope="root">
      <SkewBanner />
      <Outlet />
    </ErrorBoundary>
  )
}
