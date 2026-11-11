import { client } from "@kantharos-ai/certharbor-client/client";
import { createSession, getSession } from "@kantharos-ai/certharbor-client";
import { BASE_URL } from "./env.js";
import { Credentials, Role } from "./session.js";
import { adminCredentials, authed as authedForm, ensureUser, FIXTURE_USERS } from "../fixtures/test.js";

/**
 * Phase 4's e2e specs exercise the API through the generated TypeScript
 * client (clients/ts), not raw fetch: that is the whole point of the
 * contract pipeline (plans/frontend-backend-split.md Section 7.2).
 *
 * The package only exports one client instance (`client` from the
 * `/client` subpath, the same singleton frontend/src/api/client.ts
 * configures with `setConfig`) — `createClient` itself is not part of the
 * package's public surface. A multi-identity test suite therefore cannot
 * hold one `Client` object per role the way the SPA holds one for its one
 * logged-in user. Instead, each `ApiSession` below owns its own cookie jar
 * and its own `fetch` override, and `ApiSession.call` points the shared
 * singleton at that override for the duration of one request. The `api`
 * project runs with `workers: 1` and `fullyParallel: false`
 * (playwright.config.ts), so requests across sessions are interleaved but
 * never concurrent, which is what makes reconfiguring one shared singleton
 * before each call safe.
 */

/**
 * The generated client's `fetch` runs through Node's native undici dispatcher,
 * not Playwright's APIRequestContext -- so the `ignoreHTTPSErrors: true` every
 * other helper in this suite sets (playwright.config.ts, session.ts, oidc.ts)
 * does not reach it. Against this stack's self-signed/step-ca-issued certs,
 * plain `fetch()` fails every request with UNABLE_TO_GET_ISSUER_CERT_LOCALLY.
 * Same trust model as the rest of the e2e-only stack: accept the self-signed
 * chain rather than ship a custom undici Agent/dispatcher for one test harness.
 */
process.env.NODE_TLS_REJECT_UNAUTHORIZED = "0";

export interface ApiSession {
  cookies: Map<string, string>;
  /** Runs one generated-client call against this session's identity. */
  call<F extends () => Promise<unknown>>(fn: F): ReturnType<F>;
}

const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

function cookieHeader(jar: Map<string, string>): string {
  return [...jar.entries()].map(([name, value]) => `${name}=${value}`).join("; ");
}

/** Both the __Host- prefixed and bare spellings, prefixed first (helpers/session.ts's cookieNameCandidates). */
function firstPresent(jar: Map<string, string>, base: string): string | undefined {
  return jar.get(`__Host-${base}`) ?? jar.get(base);
}

function absorbSetCookie(jar: Map<string, string>, res: Response): void {
  const headers = res.headers as Headers & { getSetCookie?: () => string[] };
  const lines = typeof headers.getSetCookie === "function" ? headers.getSetCookie() : [];
  for (const line of lines) {
    const pair = line.split(";")[0] ?? "";
    const eq = pair.indexOf("=");
    if (eq < 0) continue;
    jar.set(pair.slice(0, eq), pair.slice(eq + 1));
  }
}

function sessionFetch(jar: Map<string, string>): typeof fetch {
  return async (input, init = {}) => {
    // The generated client (@hey-api/client-fetch) builds its own Request and
    // calls fetch(request) with no second argument, so method/headers live on
    // `input`, not `init` -- reading init.method alone silently treats every
    // call as a GET and never attaches X-CSRF-Token, which the server then
    // rejects with a 403 CSRF error indistinguishable from a bad login.
    const base = input instanceof Request ? input : new Request(input, init);
    const method = base.method.toUpperCase();
    const headers = new Headers(init.headers ?? base.headers);
    const cookie = cookieHeader(jar);
    if (cookie) headers.set("Cookie", cookie);
    if (!SAFE_METHODS.has(method)) {
      const token = firstPresent(jar, "certharbor-csrf");
      if (token) headers.set("X-CSRF-Token", token);
    }
    const finalRequest = new Request(base, { headers });
    const res = await fetch(finalRequest);
    absorbSetCookie(jar, res);
    return res;
  };
}

let configuredBaseUrl: string | undefined;

/** A fresh cookie jar plus a `call` that points the package's one client singleton at it for one request. */
export function newApiClient(baseUrl: string = BASE_URL): ApiSession {
  const jar = new Map<string, string>();
  const fetchForJar = sessionFetch(jar);
  return {
    cookies: jar,
    call<F extends () => Promise<unknown>>(fn: F): ReturnType<F> {
      if (configuredBaseUrl !== baseUrl) {
        client.setConfig({ baseUrl });
        configuredBaseUrl = baseUrl;
      }
      client.setConfig({ fetch: fetchForJar });
      return fn() as ReturnType<F>;
    },
  };
}

/**
 * Logs in the way the SPA does (GET /api/v1/session for the CSRF cookie,
 * then POST credentials), against this session's own cookie jar.
 */
export async function apiLoginOrThrow(api: ApiSession, creds: Credentials): Promise<void> {
  await api.call(() => getSession({}));
  const res = await api.call(() =>
    createSession({ body: { username: creds.username, password: creds.password } }),
  );
  const state = (res.data as { state?: string } | undefined)?.state;
  if (res.error || (state !== "authenticated" && state !== "pendingMfa")) {
    throw new Error(
      `login as ${creds.username} failed: ${res.response?.status} ${JSON.stringify(res.error ?? res.data)}`,
    );
  }
}

/** Reverse lookup from fixtures/test.ts's FIXTURE_USERS, so authedClient can bootstrap on demand. */
const FIXTURE_ROLE_BY_USERNAME: Record<string, Role> = {
  [FIXTURE_USERS.manager.username]: "manager",
  [FIXTURE_USERS.viewer.username]: "viewer",
};

/**
 * Fixture-backed specs assert state against a fresh database per run (unlike
 * fixtures/test.ts's worker-scoped admin/manager/viewer fixtures, this helper
 * is called per-spec with no shared setup), so manager_user/viewer_user are
 * never guaranteed to exist yet. Bootstrap them the same lazy way
 * fixtures/test.ts's own manager/viewer fixtures do -- via an admin session
 * and ensureUser -- before attempting the generated-client login.
 */
async function ensureFixtureUser(creds: Credentials): Promise<void> {
  const role = FIXTURE_ROLE_BY_USERNAME[creds.username];
  if (!role) return;
  const admin = await authedForm(adminCredentials());
  try {
    await ensureUser(admin, creds, role);
  } finally {
    await admin.ctx.dispose();
  }
}

/** A fresh, logged-in session. */
export async function authedClient(creds: Credentials, baseUrl: string = BASE_URL): Promise<ApiSession> {
  await ensureFixtureUser(creds);
  const api = newApiClient(baseUrl);
  await apiLoginOrThrow(api, creds);
  return api;
}

/** Reads a Blob/octet-stream response body as text, the way the old downloadCert/downloadKey helpers did over Playwright's APIResponse. */
export async function blobText(blob: unknown): Promise<string> {
  if (blob instanceof Blob) return blob.text();
  throw new Error(`expected a Blob/File body, got ${typeof blob}`);
}
