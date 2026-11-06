import { Link, Outlet } from 'react-router'
import { AuthShell } from '../auth/AuthShell'
import { useConfig } from '../api/queries'
import { useAuthenticatedUser } from '../auth/useAuthenticatedUser'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { roleAllows, type Role } from '../navigation'

// A courtesy for the person who types an address they cannot use. The server
// refuses the operations regardless.
export function RequireRole({ role, standalone = false }: { role: Role; standalone?: boolean }) {
  const user = useAuthenticatedUser()
  const config = useConfig(true)

  if (!config.data) return <Skeleton rows={3} label="Loading" />
  if (!roleAllows(user.role, role, config.data.roleLevels)) {
    const denied = (
      <ErrorState title="Access denied" problem={{ detail: 'Your role cannot open this page.' }} />
    )
    if (!standalone) return denied
    return (
      <AuthShell>
        <div className="card">
          {denied}
          <div className="auth-footer">
            <Link className="auth-link" to="/">
              Back to the application
            </Link>
          </div>
        </div>
      </AuthShell>
    )
  }
  return <Outlet />
}
