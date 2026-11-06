interface SkeletonProps {
  rows?: number
  label?: string
}

export function Skeleton({ rows = 3, label = 'Loading' }: SkeletonProps) {
  return (
    <div className="skeleton" role="status" aria-busy="true" aria-label={label} data-testid="skeleton">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="skeleton-row" />
      ))}
    </div>
  )
}
