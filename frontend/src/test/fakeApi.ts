import { client } from '@kantharos-ai/certharbor-client/client'

export type FakeSessionState = 'anonymous' | 'pendingMfa' | 'authenticated'

export interface FakeUser {
  id: number
  username: string
  role: string
}

export interface FakeCertificate {
  id: number
  name: string
  domain: string
  status: string
  keyType: string
  serial: string
  expiryStatus: string
  issuedAt: string
  expiresAt: string
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
  certificates: FakeCertificate[]
  importCandidates: { name: string; certFile: string; keyFile?: string }[]
  history: FakeHistoryEntry[]
  provisioners: { name: string; type: string }[]
  securityEvents: FakeSecurityEvent[]
  auditEvents: FakeSecurityEvent[]
  // Narrower than `down`, which also breaks the session/config calls every
  // page depends on to render at all: these fail only their own endpoint.
  // Each operation gets its own flag rather than sharing one: reusing a
  // single flag across getSession/getConfig and a page endpoint once broke
  // the former for every test exercising the latter's error state.
  historyDown: boolean
  provisionersDown: boolean
  // Dedicated flags, not the shared `down`: see historyDown's note above.
  // Reusing the global `down` flag here would also break getSession/getConfig
  // and strand these pages' tests at the Splash screen forever.
  securityEventsDown: boolean
  auditEventsDown: boolean
  dashboardDown: boolean
  // Artificial delay (ms) before any response resolves, so tests asserting
  // on a transient loading state have a window to observe it.
  delayMs: number
}

export interface FakeHistoryEntry {
  id: number
  action: string
  certName: string
  domain: string
  details: string
  username: string
  role: string
  createdAt: string
}

export interface FakeSecurityEvent {
  id: number
  username: string
  ip: string
  outcome: 'success' | 'failure'
  reason: string
  createdAt: string
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
    certificates: [
      {
        id: 1,
        name: 'example',
        domain: 'example.test',
        status: 'active',
        keyType: 'EC:P-256',
        serial: 'AA:BB:CC',
        expiryStatus: 'ok',
        issuedAt: '2026-01-01T00:00:00Z',
        expiresAt: '2027-01-01T00:00:00Z',
      },
    ],
    importCandidates: [{ name: 'orphan', certFile: 'orphan.crt', keyFile: 'orphan.key' }],
    history: [
      {
        id: 1,
        action: 'issue',
        certName: 'example',
        domain: 'example.test',
        details: 'synthetic',
        username: 'alice',
        role: 'admin',
        createdAt: '2026-01-01T00:00:00Z',
      },
    ],
    provisioners: [{ name: 'admin', type: 'JWK' }],
    securityEvents: [
      {
        id: 1,
        username: 'alice',
        ip: '203.0.113.9',
        outcome: 'failure',
        reason: 'bad password',
        createdAt: '2026-01-01T00:00:00Z',
      },
    ],
    auditEvents: [
      {
        id: 1,
        username: 'alice',
        ip: '203.0.113.9',
        outcome: 'success',
        reason: 'login',
        createdAt: '2026-01-01T00:00:00Z',
      },
    ],
    historyDown: false,
    provisionersDown: false,
    securityEventsDown: false,
    auditEventsDown: false,
    dashboardDown: false,
    delayMs: 0,
    ...overrides,
  }
  const calls: RecordedCall[] = []
  let nextCertId = Math.max(0, ...state.certificates.map((c) => c.id)) + 1

  const sessionBody = () => ({
    state: state.session,
    ...(state.session === 'authenticated' ? { user: state.user } : {}),
  })

  const handler = async (input: URL | RequestInfo, init?: RequestInit): Promise<Response> => {
    const request = input instanceof Request ? input : new Request(input, init)
    const url = new URL(request.url)
    const method = request.method
    const text = method === 'GET' || method === 'DELETE' ? '' : await request.clone().text()
    // Multipart bodies (file uploads) are not JSON; they are read via
    // request.formData() by the handler that needs them, not parsed here.
    const isJsonBody = text && !request.headers.get('Content-Type')?.includes('multipart/form-data')
    const body: unknown = isJsonBody ? JSON.parse(text) : undefined
    calls.push({ method, path: url.pathname, csrf: request.headers.get('X-CSRF-Token'), body })

    if (state.delayMs > 0) await new Promise((resolve) => setTimeout(resolve, state.delayMs))

    if (state.down === 'network') throw new TypeError('Failed to fetch')
    if (state.down)
      return new Response(state.down.body, {
        status: state.down.status,
        headers: { 'Content-Type': state.down.contentType },
      })

    const key = `${method} ${url.pathname}`
    switch (key) {
      case 'GET /api/v1/session':
        document.cookie = `certharbor-csrf=${state.csrfCookie}`
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
            'https://certharbor/errors/mfa-expired',
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
      case 'GET /api/v1/ca':
        return json({
          subject: 'CN=Fake Root CA',
          fingerprintSha256: 'ff:ee:dd',
          notBefore: '2026-01-01T00:00:00Z',
          notAfter: '2036-01-01T00:00:00Z',
          reachable: true,
        })
      case 'GET /api/v1/ca/root':
      case 'GET /api/v1/ca/intermediate':
      case 'GET /api/v1/ca/chain':
        return new Response('fake-pem-bytes', {
          status: 200,
          headers: { 'Content-Type': 'application/x-pem-file' },
        })
      case 'GET /api/v1/certificates': {
        const q = (url.searchParams.get('q') ?? '').toLowerCase()
        const statusFilter = url.searchParams.get('status') ?? undefined
        const items = state.certificates.filter(
          (c) =>
            (!q || c.name.toLowerCase().includes(q) || c.domain.toLowerCase().includes(q)) &&
            (!statusFilter || c.status === statusFilter),
        )
        return json({ items, page: 1, pageSize: 25, total: items.length, totalPages: 1 })
      }
      case 'POST /api/v1/certificates': {
        const created = body as { name: string; domain: string; keyType?: string }
        const cert: FakeCertificate = {
          id: nextCertId++,
          name: created.name,
          domain: created.domain,
          status: 'active',
          keyType: created.keyType || 'EC:P-256',
          serial: 'NEW:SERIAL',
          expiryStatus: 'ok',
          issuedAt: new Date().toISOString(),
          expiresAt: new Date().toISOString(),
        }
        state.certificates.push(cert)
        return json(cert, 201)
      }
      case 'GET /api/v1/certificates/options':
        return json({
          templates: [
            {
              id: 'server',
              label: 'Server',
              description: 'Server cert',
              defaultDuration: '8760h',
              defaultKeyType: 'EC:P-256',
            },
          ],
          keyTypes: [{ id: 'EC:P-256', label: 'EC P-256' }],
          durations: [{ id: '8760h', label: '1 year' }],
          provisioners: [],
          allowedDomainSuffixes: [],
          defaultTemplate: 'server',
        })
      case 'GET /api/v1/certificates/import-candidates':
        return json(state.importCandidates)
      case 'GET /api/v1/history': {
        if (state.historyDown) return problem(500, 'Internal Server Error', 'history unavailable')
        const actions = url.searchParams.getAll('action').filter((a) => a !== '')
        const cert = url.searchParams.get('cert') ?? ''
        const page = Number(url.searchParams.get('page') ?? '1')
        const pageSize = Number(url.searchParams.get('pageSize') ?? '25')
        const filtered = state.history.filter(
          (e) => (actions.length === 0 || actions.includes(e.action)) && (!cert || e.certName === cert),
        )
        const start = (page - 1) * pageSize
        const items = filtered.slice(start, start + pageSize)
        return json({
          items,
          page,
          pageSize,
          total: filtered.length,
          totalPages: Math.max(1, Math.ceil(filtered.length / pageSize)),
        })
      }
      case 'GET /api/v1/provisioners':
        if (state.provisionersDown) return problem(500, 'Internal Server Error', 'provisioners unavailable')
        return json(state.provisioners)
      case 'GET /api/v1/security/events': {
        if (state.securityEventsDown)
          return problem(500, 'Internal Server Error', 'security events unavailable')
        const q = (url.searchParams.get('q') ?? '').toLowerCase()
        const outcomeFilter = url.searchParams.get('outcome') ?? ''
        const page = Number(url.searchParams.get('page') ?? '1')
        const pageSize = Number(url.searchParams.get('pageSize') ?? '25')
        const filtered = state.securityEvents.filter(
          (e) =>
            (!q || e.username.toLowerCase().includes(q) || e.ip.toLowerCase().includes(q)) &&
            (!outcomeFilter || e.outcome === outcomeFilter),
        )
        const start = (page - 1) * pageSize
        const items = filtered.slice(start, start + pageSize)
        return json({
          items,
          page,
          pageSize,
          total: filtered.length,
          totalPages: Math.max(1, Math.ceil(filtered.length / pageSize)),
        })
      }
      case 'GET /api/v1/audit-events': {
        if (state.auditEventsDown) return problem(500, 'Internal Server Error', 'audit events unavailable')
        const page = Number(url.searchParams.get('page') ?? '1')
        const pageSize = Number(url.searchParams.get('pageSize') ?? '25')
        const start = (page - 1) * pageSize
        const items = state.auditEvents.slice(start, start + pageSize)
        return json({
          items,
          page,
          pageSize,
          total: state.auditEvents.length,
          totalPages: Math.max(1, Math.ceil(state.auditEvents.length / pageSize)),
        })
      }
      case 'GET /api/v1/dashboard': {
        if (state.dashboardDown) return problem(500, 'Internal Server Error', 'dashboard unavailable')
        const byStatus = { ok: 0, warning: 0, expired: 0 } as Record<string, number>
        for (const c of state.certificates) byStatus[c.expiryStatus] = (byStatus[c.expiryStatus] ?? 0) + 1
        return json({
          certificates: {
            total: state.certificates.length,
            byStatus,
            expiringSoon: byStatus.warning ?? 0,
          },
          ca: {
            subject: 'CN=Fake Root CA',
            fingerprintSha256: 'ff:ee:dd',
            notBefore: '2026-01-01T00:00:00Z',
            notAfter: '2036-01-01T00:00:00Z',
            reachable: true,
          },
        })
      }
      case 'POST /api/v1/certificates/import/paths': {
        const items = body as { name: string; domain: string; certPath: string }[]
        return json({ results: items.map((i) => ({ ref: i.name, status: 'ok' as const })) })
      }
      case 'POST /api/v1/certificates/import/upload': {
        const form = await request.formData()
        const name = String(form.get('name') ?? 'uploaded')
        const domain = String(form.get('domain') ?? '')
        const cert: FakeCertificate = {
          id: nextCertId++,
          name,
          domain,
          status: 'active',
          keyType: 'EC:P-256',
          serial: 'UPLOAD:SERIAL',
          expiryStatus: 'ok',
          issuedAt: new Date().toISOString(),
          expiresAt: new Date().toISOString(),
        }
        state.certificates.push(cert)
        return json(cert, 201)
      }
      default:
        break
    }

    const ipBlockMatch = /^\/api\/v1\/security\/ip-blocks\/(.+)$/.exec(url.pathname)
    if (ipBlockMatch && method === 'DELETE') {
      // url.pathname is already decoded, so '::1' round-trips as '::1' here,
      // not '%3A%3A1' -- the encoding only matters on the wire, which is
      // exactly what the generated client is trusted to get right; this fake
      // only needs to accept whatever the real server would.
      const ip = decodeURIComponent(ipBlockMatch[1]!)
      state.securityEvents = state.securityEvents.filter((e) => e.ip !== ip)
      return new Response(null, { status: 204 })
    }

    const idMatch = /^\/api\/v1\/certificates\/(\d+)(\/(certificate|key|renew|revoke))?$/.exec(url.pathname)
    if (idMatch) {
      const id = Number(idMatch[1])
      const action = idMatch[3]
      const cert = state.certificates.find((c) => c.id === id)
      if (!cert) return problem(404, 'Not Found', 'no such certificate')
      if (method === 'GET' && !action) {
        return json({
          daysLeft: 300,
          dnsNames: [cert.domain],
          extKeyUsage: ['serverAuth'],
          fingerprintSha1: 'sha1:fake',
          fingerprintSha256: 'sha256:fake',
          ipAddresses: [],
          issuer: 'CN=Fake Root CA',
          keyUsage: ['digitalSignature'],
          notAfter: cert.expiresAt,
          notBefore: cert.issuedAt,
          publicKey: 'fake-public-key',
          serialHex: cert.serial,
          signatureAlgorithm: 'ECDSA-SHA256',
          subject: `CN=${cert.domain}`,
          validations: [{ name: 'chain', status: 'ok', detail: 'Chain verifies.' }],
        })
      }
      if (method === 'GET' && (action === 'certificate' || action === 'key')) {
        return new Response('fake-pem-bytes', {
          status: 200,
          headers: { 'Content-Type': 'application/x-pem-file' },
        })
      }
      if (method === 'POST' && action === 'renew') {
        cert.issuedAt = new Date().toISOString()
        return json(cert)
      }
      if (method === 'POST' && action === 'revoke') {
        cert.status = 'revoked'
        return json(cert)
      }
    }

    return problem(404, 'Not Found', 'no operation matches this path')
  }

  client.setConfig({ baseUrl: 'http://localhost', fetch: handler })
  return { state, calls }
}
