import { createContext, useContext } from 'react'

export interface AuthenticatedUser {
  id: number
  username: string
  role: string
}

export const AuthenticatedUserContext = createContext<AuthenticatedUser | null>(null)

// Supplied by <AuthGate>, not read from the session query. A component below
// the gate that observed the query itself could render once more after the
// session turned anonymous and before the gate unmounted it.
export function useAuthenticatedUser(): AuthenticatedUser {
  const user = useContext(AuthenticatedUserContext)
  if (!user) throw new Error('useAuthenticatedUser used outside <AuthGate>')
  return user
}
