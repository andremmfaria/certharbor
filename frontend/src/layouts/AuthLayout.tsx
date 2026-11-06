import { Outlet } from 'react-router'
import { AuthShell } from '../auth/AuthShell'
import { ErrorBoundary } from '../components/ErrorBoundary'

// Shared by login, forgot-password and reset-password (D12.8).
export function AuthLayout() {
  return (
    <AuthShell>
      <ErrorBoundary scope="auth-layout">
        <Outlet />
      </ErrorBoundary>
    </AuthShell>
  )
}
