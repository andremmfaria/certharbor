import type { ReactNode } from 'react'

interface EmptyStateProps {
  title: string
  children?: ReactNode
}

export function EmptyState({ title, children }: EmptyStateProps) {
  return (
    <div className="state state-empty" data-testid="empty-state">
      <div className="state-title">{title}</div>
      {children ? <div className="state-body">{children}</div> : null}
    </div>
  )
}
