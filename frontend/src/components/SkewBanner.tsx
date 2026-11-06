import { buildContractSha } from '../contractSha'
import { useConfig, useSession } from '../api/queries'

// Persistent and non-blocking: a false positive that bricks the UI is worse
// than the skew it detects (5.10). Silent when the build carries no digest,
// which is `vite dev`.
export function SkewBanner() {
  const session = useSession()
  const config = useConfig(!session.isPending)
  const built = buildContractSha()

  if (!built || !config.data) return null
  if (config.data.contractSha === built) return null

  return (
    <div className="skew-banner" role="status" data-testid="skew-banner">
      This page was built for a different version of the server. Reload to pick up the latest.
      <button type="button" className="btn-sm" onClick={() => window.location.reload()}>
        Reload
      </button>
    </div>
  )
}
