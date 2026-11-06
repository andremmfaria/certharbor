import { Link } from 'react-router'
import { EmptyState } from '../components/EmptyState'

export function NotFoundPage() {
  return (
    <section data-testid="page-not-found">
      <EmptyState title="Page not found">
        <Link to="/">Back to the dashboard</Link>
      </EmptyState>
    </section>
  )
}
