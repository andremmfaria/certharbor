import { Component, type ErrorInfo, type ReactNode } from 'react'
import { ErrorState } from './ErrorState'

interface ErrorBoundaryProps {
  children: ReactNode
  scope: string
}

interface ErrorBoundaryState {
  failed: boolean
}

// Query errors are values (throwOnError is false), so this only ever catches a
// render crash. One at the root and one per layout keeps a broken page from
// taking the navigation down with it.
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  override state: ErrorBoundaryState = { failed: false }

  static getDerivedStateFromError(): ErrorBoundaryState {
    return { failed: true }
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error(`render crash in ${this.props.scope}`, error, info.componentStack)
  }

  override render() {
    if (!this.state.failed) return this.props.children
    return <ErrorState title="This page failed to render" onRetry={() => this.setState({ failed: false })} />
  }
}
