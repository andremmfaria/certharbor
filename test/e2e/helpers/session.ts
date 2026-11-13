import { APIRequestContext, APIResponse, request } from "@playwright/test";
import { BASE_URL } from "./env.js";

export type Role = "viewer" | "manager" | "admin";

export interface Credentials {
  username: string;
  password: string;
}

/**
 * "Jar A" and "jar B" in Section 3 mean two independent contexts with their own
 * cookie storage. Several properties only hold across two of them (E2E-CSRF-05,
 * E2E-AUTH-15), so the isolation is part of the assertion.
 */
export async function newJar(baseURL = BASE_URL): Promise<APIRequestContext> {
  return request.newContext({ baseURL, ignoreHTTPSErrors: true, maxRedirects: 0 });
}

/**
 * Mint or read the session-bound CSRF pair through the API. The url argument is
 * retained temporarily so legacy callers compile while their endpoint-specific
 * form requests are migrated; it is deliberately not fetched because SPA
 * document routes never render a token.
 */
export async function csrfTokenFrom(ctx: APIRequestContext, _url = "/api/v1/session"): Promise<string> {
  const res = await ctx.get("/api/v1/session");
  if (res.status() !== 200) {
    throw new Error(`GET /api/v1/session for CSRF returned ${res.status()}: ${(await res.text()).slice(0, 400)}`);
  }
  return csrfToken(ctx);
}

export function extractCSRF(html: string, whence = "<page>"): string {
  const m = /name="csrf_token"\s+value="([^"]+)"/.exec(html);
  if (!m || !m[1]) {
    throw new Error(`no csrf_token rendered in ${whence}; body starts: ${html.slice(0, 400)}`);
  }
  return m[1];
}

/** The server-rendered login form, for the specs that assert its own behaviour (re-render, flash, rate limit). */
export async function login(ctx: APIRequestContext, creds: Credentials): Promise<APIResponse> {
  const token = await csrfTokenFrom(ctx, "/login");
  return ctx.post("/login", {
    form: { username: creds.username, password: creds.password, csrf_token: token },
  });
}

/**
 * The JSON login the SPA uses (createSession). GET /api/v1/session issues the
 * CSRF cookie the POST must echo in X-CSRF-Token (5.4).
 */
export async function apiLogin(ctx: APIRequestContext, creds: Credentials): Promise<APIResponse> {
  await ctx.get("/api/v1/session");
  const token = await csrfToken(ctx);
  return ctx.post("/api/v1/session", {
    data: { username: creds.username, password: creds.password },
    headers: { "X-CSRF-Token": token },
  });
}

/**
 * Logs in through the API, so no fixture depends on a server-rendered page.
 * A user with MFA enrolled stops at pendingMfa, which counts: the form login
 * this replaced accepted the redirect to the code step the same way.
 */
export async function loginOrThrow(ctx: APIRequestContext, creds: Credentials): Promise<void> {
  const res = await apiLogin(ctx, creds);
  const body = await res.text();
  let state = "";
  try {
    state = (JSON.parse(body) as { state?: string }).state ?? "";
  } catch {
    // Reported below with the body.
  }
  if (res.status() !== 200 || (state !== "authenticated" && state !== "pendingMfa")) {
    throw new Error(`login as ${creds.username} failed with ${res.status()}: ${body.slice(0, 400)}`);
  }
}

export async function apiMutation(
  ctx: APIRequestContext,
  method: "POST" | "PUT" | "PATCH" | "DELETE",
  url: string,
  data: unknown = {},
  token?: string,
): Promise<APIResponse> {
  return ctx.fetch(url, {
    method,
    data,
    headers: { "X-CSRF-Token": token ?? (await csrfToken(ctx)) },
  });
}

export async function logout(ctx: APIRequestContext): Promise<APIResponse> {
  return apiMutation(ctx, "DELETE", "/api/v1/session");
}

/**
 * Standing rule from Section 4.1.4: on a 302 to /login (or an API 401) from a fixture-backed
 * context, re-run the login once and retry the request once, rather than
 * surfacing the redirect as the test's own result. Nothing in Section 3 logs
 * the shared fixture users out, but a role change or a session-epoch bump on
 * one of them would otherwise break every later test in the same worker.
 */
export async function withLazyReauth<T extends APIResponse>(
  ctx: APIRequestContext,
  creds: Credentials,
  send: () => Promise<T>,
): Promise<T> {
  const first = await send();
  if (!redirectsToLogin(first)) return first;
  await loginOrThrow(ctx, creds);
  return send();
}

/** A server-rendered route redirects an expired session to /login; an /api/v1 operation answers 401 instead. */
export function redirectsToLogin(res: APIResponse): boolean {
  if (res.status() === 401 && new URL(res.url()).pathname.startsWith("/api/")) return true;
  if (res.status() !== 302 && res.status() !== 303) return false;
  const location = res.headers()["location"] ?? "";
  return location.startsWith("/login");
}

/**
 * Cookie name bases. The served names carry the __Host- prefix whenever
 * SESSION_SECURE is on (D6 of plans/frontend-backend-split.md), so the suite
 * must not assume either spelling: it probes prefixed first, then bare, the
 * same way src/api/client.ts does.
 */
export const SESSION_COOKIE_BASE = "certharbor";
export const CSRF_COOKIE_BASE = "certharbor-csrf";
export const OIDC_COOKIE_BASE = "certharbor-oidc";
const HOST_PREFIX = "__Host-";

/** Both spellings of a cookie base, prefixed first. */
export function cookieNameCandidates(base: string): string[] {
  return [`${HOST_PREFIX}${base}`, base];
}

/**
 * The session cookie's served name, resolved against a cookie jar. Prefer this
 * over a literal: which spelling is served depends on SESSION_SECURE, and the
 * e2e stack does not run with the same value in every scenario.
 */
export async function sessionCookieName(ctx: APIRequestContext): Promise<string> {
  const state = await ctx.storageState();
  for (const name of cookieNameCandidates(SESSION_COOKIE_BASE)) {
    if (state.cookies.some((c) => c.name === name)) return name;
  }
  throw new Error(
    `no session cookie in jar; cookies present: ${state.cookies.map((c) => c.name).join(", ")}`,
  );
}

/**
 * Deprecated: the served name is no longer a constant. Kept so existing specs
 * keep compiling while they are migrated to sessionCookieName().
 */
export const SESSION_COOKIE_NAME = SESSION_COOKIE_BASE;

/** The raw session cookie, which E2E-AUTH-12 captures and replays after a logout. */
export async function sessionCookie(ctx: APIRequestContext): Promise<string> {
  const state = await ctx.storageState();
  const names = cookieNameCandidates(SESSION_COOKIE_BASE);
  const cookie = state.cookies.find((c) => names.includes(c.name));
  if (!cookie) {
    throw new Error(
      `no session cookie in jar (tried ${names.join(", ")}); cookies present: ${state.cookies.map((c) => c.name).join(", ")}`,
    );
  }
  return `${cookie.name}=${cookie.value}`;
}

/**
 * The readable CSRF token the SPA echoes in X-CSRF-Token (5.4). It is set on
 * login as well as by GET /api/v1/session.
 */
export async function csrfToken(ctx: APIRequestContext): Promise<string> {
  const state = await ctx.storageState();
  const names = cookieNameCandidates(CSRF_COOKIE_BASE);
  const cookie = state.cookies.find((c) => names.includes(c.name));
  if (!cookie) {
    throw new Error(
      `no CSRF cookie in jar (tried ${names.join(", ")}); cookies present: ${state.cookies.map((c) => c.name).join(", ")}`,
    );
  }
  return cookie.value;
}
