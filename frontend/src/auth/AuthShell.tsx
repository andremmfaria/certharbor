import type { ReactNode } from 'react'

export function AuthShell({ children }: { children: ReactNode }) {
  return (
    <div className="auth-shell">
      <div className="login-wrap">
        <div className="logo">
          <h1>&#x1F510; Step-CA</h1>
          <span>Certificate Manager</span>
        </div>
        {children}
      </div>
    </div>
  )
}
