import { Navigate, Outlet, useLocation } from 'react-router'
import { useSession } from '../api/queries'
import { Splash } from '../components/Splash'
import { AuthShell } from './AuthShell'
import { Unavailable } from './Unavailable'
import { AuthenticatedUserContext } from './useAuthenticatedUser'

// Branches on the state discriminator the session endpoint returns (5.3),
// never on a 401, so an expired session and a missing one are the same case.
export function AuthGate() {
  const session = useSession()
  const location = useLocation()

  const unavailable = (
    <AuthShell>
      <Unavailable onRetry={() => void session.refetch()} />
    </AuthShell>
  )

  if (session.isPending) return <Splash />
  if (!session.data) return unavailable

  switch (session.data.state) {
    case 'authenticated':
      if (!session.data.user) return unavailable
      return (
        <AuthenticatedUserContext.Provider value={session.data.user}>
          <Outlet />
        </AuthenticatedUserContext.Provider>
      )
    case 'anonymous':
    case 'pendingMfa':
      return <Navigate to="/login" replace state={{ from: location }} />
  }
}
