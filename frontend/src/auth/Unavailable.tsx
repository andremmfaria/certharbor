// Rendered when the boot call cannot reach a working backend (5.10). It is a
// retry, deliberately not a login form: the form would accept a password it
// cannot send.
export function Unavailable({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="card" data-testid="unavailable">
      <h2>Service unavailable</h2>
      <div className="error" role="alert">
        The server is not responding. Your session is unaffected.
      </div>
      <button type="button" className="btn" onClick={onRetry}>
        Try again
      </button>
    </div>
  )
}
