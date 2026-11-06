import { errorMessage } from '../api/errors'

interface ErrorStateProps {
  problem?: unknown
  title?: string
  onRetry?: () => void
}

export function ErrorState({ problem, title = 'Something went wrong', onRetry }: ErrorStateProps) {
  return (
    <div className="state state-error" role="alert" data-testid="error-state">
      <div className="state-title">{title}</div>
      <div className="state-body">{errorMessage(problem)}</div>
      {onRetry ? (
        <button type="button" className="btn-sm" onClick={onRetry}>
          Try again
        </button>
      ) : null}
    </div>
  )
}
