import { client } from '@certharbor/client/client'
import { isAuthEndpoint } from './authEndpoints'
import { readCsrf } from './csrf'
import { recordSessionExpiry, SESSION_EXPIRES_HEADER } from './sessionExpiry'

const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

export function csrfRequestInterceptor(read: () => string | undefined = readCsrf) {
  return (req: Request): Request => {
    const token = read()
    if (token && !SAFE_METHODS.has(req.method.toUpperCase())) req.headers.set('X-CSRF-Token', token)
    return req
  }
}

export interface UnauthorizedHandlers {
  clearCache: () => void
  redirectToLogin: () => void
}

// 401 means "the session is gone" everywhere except the operations that
// return their own 401 as a value. 403 never redirects: it is a role or CSRF
// refusal, not an expiry.
export function unauthorizedResponseInterceptor({ clearCache, redirectToLogin }: UnauthorizedHandlers) {
  return (res: Response, req: Request): Response => {
    recordSessionExpiry(res.headers.get(SESSION_EXPIRES_HEADER))
    if (res.status === 401 && !isAuthEndpoint(req.method, req.url)) {
      clearCache()
      redirectToLogin()
    }
    return res
  }
}

// baseUrl is empty because nginx (Vite in dev) serves the document and proxies
// the API on one origin, so every request is same-origin and relative.
export function installClient(handlers: UnauthorizedHandlers): () => void {
  client.setConfig({ baseUrl: '', credentials: 'same-origin' })
  const request = client.interceptors.request.use(csrfRequestInterceptor())
  const response = client.interceptors.response.use(unauthorizedResponseInterceptor(handlers))
  return () => {
    client.interceptors.request.eject(request)
    client.interceptors.response.eject(response)
  }
}
