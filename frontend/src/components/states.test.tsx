import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { EmptyState } from './EmptyState'
import { ErrorBoundary } from './ErrorBoundary'
import { ErrorState } from './ErrorState'
import { Skeleton } from './Skeleton'

describe('state primitives', () => {
  it('renders a skeleton that announces itself as busy', () => {
    render(<Skeleton rows={2} />)
    expect(screen.getByRole('status')).toHaveAttribute('aria-busy', 'true')
  })

  it('renders an empty state with a title and hint', () => {
    render(<EmptyState title="No certificates">Issue one to get started.</EmptyState>)
    expect(screen.getByText('No certificates')).toBeInTheDocument()
    expect(screen.getByText('Issue one to get started.')).toBeInTheDocument()
  })

  it('renders a problem document as an alert and retries on request', async () => {
    const retry = vi.fn()
    render(<ErrorState problem={{ detail: 'database unreachable' }} onRetry={retry} />)

    expect(screen.getByRole('alert')).toHaveTextContent('database unreachable')
    await userEvent.click(screen.getByRole('button', { name: /try again/i }))
    expect(retry).toHaveBeenCalled()
  })

  it('catches a render crash and recovers when asked', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    let crash = true
    function Bomb() {
      if (crash) throw new Error('boom')
      return <div>fine</div>
    }
    render(
      <ErrorBoundary scope="test">
        <Bomb />
      </ErrorBoundary>,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('This page failed to render')

    crash = false
    await userEvent.click(screen.getByRole('button', { name: /try again/i }))
    expect(screen.getByText('fine')).toBeInTheDocument()
  })
})
