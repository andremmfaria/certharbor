export interface AuthEndpoint {
  method: string
  path: string
}

// Operations whose 401 is a value for the caller (a wrong password, a wrong
// code), not a signal that the session expired. The 401 interceptor and its
// test both read this list so the two cannot diverge.
export const AUTH_ENDPOINTS: readonly AuthEndpoint[] = [
  { method: 'POST', path: '/api/v1/session' },
  { method: 'POST', path: '/api/v1/session/mfa' },
  { method: 'POST', path: '/api/v1/password-reset/requests' },
  { method: 'POST', path: '/api/v1/password-reset/validate' },
  { method: 'POST', path: '/api/v1/password-reset/confirm' },
]

export function isAuthEndpoint(method: string, url: string): boolean {
  const path = new URL(url, 'http://localhost').pathname
  const verb = method.toUpperCase()
  return AUTH_ENDPOINTS.some((e) => e.method === verb && e.path === path)
}
