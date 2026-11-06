import { useAuthenticatedUser } from '../auth/useAuthenticatedUser'
import { EmptyState } from '../components/EmptyState'

export function DashboardPage() {
  const user = useAuthenticatedUser()
  return (
    <section data-testid="page-dashboard">
      <div className="page-header">
        <div>
          <div className="breadcrumb">dashboard</div>
          <h1>Welcome, {user.username}</h1>
        </div>
      </div>
      <EmptyState title="Nothing here yet">The dashboard has not been built.</EmptyState>
    </section>
  )
}
