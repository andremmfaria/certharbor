import { getSession, createSession } from '@andremmfaria/step-ca-webui-client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AUTH_ENDPOINTS, isAuthEndpoint } from './authEndpoints'
import { csrfRequestInterceptor, installClient, unauthorizedResponseInterceptor } from './client'
import { getSessionExpiry, clearSessionExpiry } from './sessionExpiry'
import { installFakeApi } from '../test/fakeApi'

const origin = 'http://localhost'

function unauthorizedHandlers() {
  return { clearCache: vi.fn(), redirectToLogin: vi.fn() }
}

describe('scoped 401 interceptor', () => {
  it.each(AUTH_ENDPOINTS)('returns a 401 from $method $path to the caller as a value', ({ method, path }) => {
    const handlers = unauthorizedHandlers()
    const intercept = unauthorizedResponseInterceptor(handlers)
    const res = new Response(null, { status: 401 })

    expect(intercept(res, new Request(origin + path, { method }))).toBe(res)
    expect(handlers.clearCache).not.toHaveBeenCalled()
    expect(handlers.redirectToLogin).not.toHaveBeenCalled()
  })

  it('clears the cache and redirects on a 401 from any other operation', () => {
    const handlers = unauthorizedHandlers()
    const intercept = unauthorizedResponseInterceptor(handlers)

    intercept(new Response(null, { status: 401 }), new Request(origin + '/api/v1/status'))

    expect(handlers.clearCache).toHaveBeenCalledTimes(1)
    expect(handlers.redirectToLogin).toHaveBeenCalledTimes(1)
  })

  it('does not treat the same path under a different method as an auth endpoint', () => {
    const handlers = unauthorizedHandlers()
    const intercept = unauthorizedResponseInterceptor(handlers)

    intercept(
      new Response(null, { status: 401 }),
      new Request(origin + '/api/v1/session', { method: 'DELETE' }),
    )

    expect(handlers.redirectToLogin).toHaveBeenCalledTimes(1)
  })

  it.each([403, 404, 422, 429, 500, 503])('never redirects on a %i', (status) => {
    const handlers = unauthorizedHandlers()
    unauthorizedResponseInterceptor(handlers)(
      new Response(null, { status }),
      new Request(origin + '/api/v1/status'),
    )

    expect(handlers.clearCache).not.toHaveBeenCalled()
    expect(handlers.redirectToLogin).not.toHaveBeenCalled()
  })

  it('matches by path whatever the host', () => {
    expect(isAuthEndpoint('post', 'https://example.test/api/v1/session/mfa')).toBe(true)
    expect(isAuthEndpoint('POST', '/api/v1/session?x=1')).toBe(true)
    expect(isAuthEndpoint('POST', '/api/v1/session/mfa/extra')).toBe(false)
  })

  it('records the latest session expiry off any response', () => {
    clearSessionExpiry()
    const res = new Response(null, {
      status: 200,
      headers: { 'X-Session-Expires-At': '2030-01-01T00:00:00Z' },
    })
    unauthorizedResponseInterceptor(unauthorizedHandlers())(res, new Request(origin + '/api/v1/status'))
    expect(getSessionExpiry()?.toISOString()).toBe('2030-01-01T00:00:00.000Z')
  })
})

describe('csrf request interceptor', () => {
  it('sets X-CSRF-Token on unsafe methods only', () => {
    const intercept = csrfRequestInterceptor(() => 'tok')
    expect(intercept(new Request(origin + '/x', { method: 'POST' })).headers.get('X-CSRF-Token')).toBe('tok')
    expect(intercept(new Request(origin + '/x', { method: 'DELETE' })).headers.get('X-CSRF-Token')).toBe(
      'tok',
    )
    expect(intercept(new Request(origin + '/x', { method: 'GET' })).headers.get('X-CSRF-Token')).toBeNull()
  })

  it('sets nothing when there is no cookie', () => {
    const req = csrfRequestInterceptor(() => undefined)(new Request(origin + '/x', { method: 'POST' }))
    expect(req.headers.get('X-CSRF-Token')).toBeNull()
  })
})

describe('installed on the generated client', () => {
  let eject: () => void
  const handlers = unauthorizedHandlers()

  beforeEach(() => {
    handlers.clearCache.mockClear()
    handlers.redirectToLogin.mockClear()
  })
  afterEach(() => {
    eject()
    document.cookie = 'step-webui-csrf=; Max-Age=0'
  })

  it('attaches the cookie to a mutation and not to a read', async () => {
    eject = installClient(handlers)
    // installClient points baseUrl at the page origin, which the fake must override.
    const { calls } = installFakeApi({ password: 'pw', csrfCookie: 'cookie-value' })

    await getSession()
    await createSession({ body: { username: 'a', password: 'pw' } })

    expect(calls.map((c) => [c.method, c.csrf])).toEqual([
      ['GET', null],
      ['POST', 'cookie-value'],
    ])
  })

  it('hands a wrong password back as a value without clearing anything', async () => {
    eject = installClient(handlers)
    installFakeApi({ password: 'right' })

    const res = await createSession({ body: { username: 'a', password: 'wrong' } })

    expect(res.response?.status).toBe(401)
    expect(res.error).toMatchObject({ status: 401 })
    expect(handlers.clearCache).not.toHaveBeenCalled()
    expect(handlers.redirectToLogin).not.toHaveBeenCalled()
  })

  it('redirects when an ordinary operation answers 401', async () => {
    eject = installClient(handlers)
    installFakeApi()
    const { client } = await import('@andremmfaria/step-ca-webui-client/client')
    client.setConfig({
      fetch: () => Promise.resolve(new Response(JSON.stringify({ status: 401 }), { status: 401 })),
    })

    const { getStatus } = await import('@andremmfaria/step-ca-webui-client')
    await getStatus()

    expect(handlers.clearCache).toHaveBeenCalledTimes(1)
    expect(handlers.redirectToLogin).toHaveBeenCalledTimes(1)
  })
})
