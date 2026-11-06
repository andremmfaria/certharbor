import { client } from '@andremmfaria/step-ca-webui-client/client'

export type FakeSessionState = 'anonymous' | 'pendingMfa' | 'authenticated'

export interface FakeUser {
  id: number
  username: string
  role: string
}

export interface FakeApiState {
  session: FakeSessionState
  user: FakeUser
  contractSha: string
  oidcEnabled: boolean
  localLoginEnabled: boolean
  // When set, every request answers with this status instead of its normal reply.
  down: { status: number; body: string; contentType: string } | 'network' | undefined
  password: string
  mfaCode: string
  recoveryCode: string
  mfaRequired: boolean
  csrfCookie: string
}

export interface RecordedCall {
  method: string
  path: string
  csrf: string | null
  body: unknown
}

const json = (body: unknown, status = 200, contentType = 'application/json') =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': contentType } })

const problem = (status: number, title: string, detail: string, type = 'about:blank') =>
  json({ status, title, detail, type }, status, 'application/problem+json')

export const ROLE_LEVELS = { viewer: 1, manager: 2, admin: 3 }

// A stand-in for the backend with just enough memory to drive the login flow:
// the discriminated session, the CSRF cookie and the two credential steps.
export function installFakeApi(overrides: Partial<FakeApiState> = {}) {
  const state: FakeApiState = {
    session: 'anonymous',
    user: { id: 1, username: 'alice', role: 'admin' },
    contractSha: 'abc',
    oidcEnabled: false,
    localLoginEnabled: true,
    down: undefined,
    password: 'correct horse',
    mfaCode: '123456',
    recoveryCode: 'RECOVERY-CODE',
    mfaRequired: false,
    csrfCookie: 'csrf-token',
    ...overrides,
  }
  const calls: RecordedCall[] = []

  const sessionBody = () => ({
    state: state.session,
    ...(state.session === 'authenticated' ? { user: state.user } : {}),
  })

  const handler = async (input: URL | RequestInfo, init?: RequestInit): Promise<Response> => {
    const request = input instanceof Request ? input : new Request(input, init)
    const url = new URL(request.url)
    const method = request.method
    const text = method === 'GET' || method === 'DELETE' ? '' : await request.clone().text()
    const body: unknown = text ? JSON.parse(text) : undefined
    calls.push({ method, path: url.pathname, csrf: request.headers.get('X-CSRF-Token'), body })

    if (state.down === 'network') throw new TypeError('Failed to fetch')
    if (state.down)
      return new Response(state.down.body, {
        status: state.down.status,
        headers: { 'Content-Type': state.down.contentType },
      })

    const key = `${method} ${url.pathname}`
    switch (key) {
      case 'GET /api/v1/session':
        document.cookie = `step-webui-csrf=${state.csrfCookie}`
        return json(sessionBody())
      case 'GET /api/v1/config':
        return json({
          oidcEnabled: state.oidcEnabled,
          localLoginEnabled: state.localLoginEnabled,
          oidcButtonLabel: state.oidcEnabled ? 'Continue with SSO' : '',
          acmeEnabled: true,
          appVersion: '0.0.0',
          contractSha: state.contractSha,
          roleLevels: ROLE_LEVELS,
          sessionIdleTimeoutSeconds: 28800,
          expiringSoonDays: 30,
        })
      case 'POST /api/v1/session': {
        const credentials = body as { username: string; password: string }
        if (credentials.password !== state.password) {
          return problem(401, 'Unauthorized', 'Invalid credentials. Attempts remaining: 4')
        }
        state.session = state.mfaRequired ? 'pendingMfa' : 'authenticated'
        return json(sessionBody())
      }
      case 'POST /api/v1/session/mfa': {
        if (state.session !== 'pendingMfa') {
          return problem(
            401,
            'Unauthorized',
            'No sign-in is waiting for a code.',
            'https://step-ca-webui/errors/mfa-expired',
          )
        }
        const submitted = body as { code: string; codeType: 'totp' | 'recovery' }
        const expected = submitted.codeType === 'recovery' ? state.recoveryCode : state.mfaCode
        if (submitted.code !== expected)
          return problem(401, 'Unauthorized', 'Invalid credentials. Attempts remaining: 4')
        state.session = 'authenticated'
        return json(sessionBody())
      }
      case 'DELETE /api/v1/session':
        state.session = 'anonymous'
        return new Response(null, { status: 204 })
      default:
        return problem(404, 'Not Found', 'no operation matches this path')
    }
  }

  client.setConfig({ baseUrl: 'http://localhost', fetch: handler })
  return { state, calls }
}
