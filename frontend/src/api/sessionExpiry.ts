import { useSyncExternalStore } from 'react'

let expiresAt: Date | undefined
const listeners = new Set<() => void>()

export const SESSION_EXPIRES_HEADER = 'X-Session-Expires-At'

// The only source for an expiry countdown: the session returned at boot is
// stale a minute later, and every /api/v1 response carries a fresh value (5.3).
export function recordSessionExpiry(header: string | null): void {
  if (!header) return
  const parsed = new Date(header)
  if (Number.isNaN(parsed.getTime())) return
  expiresAt = parsed
  listeners.forEach((l) => l())
}

export function clearSessionExpiry(): void {
  expiresAt = undefined
  listeners.forEach((l) => l())
}

export function getSessionExpiry(): Date | undefined {
  return expiresAt
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useSessionExpiry(): Date | undefined {
  return useSyncExternalStore(subscribe, getSessionExpiry)
}
