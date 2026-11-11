import { deleteIpBlock, listSecurityEvents } from "@kantharos-ai/certharbor-client";
import { exec, psql } from "../../helpers/compose.js";
import { ApiSession, authedClient, newApiClient } from "../../helpers/apiClient.js";
import { adminCredentials } from "../../fixtures/test.js";
import { expect, test } from "@playwright/test";

// Rewritten against GET /api/v1/security/events and DELETE
// /api/v1/security/ip-blocks/{ip} through the generated client (clients/ts),
// the same pipeline hist.api.spec.ts was rewritten against.
// sec-01.api.spec.ts keeps covering the still-live template route
// (/admin/security, templateRouteRetired=false); this file is the new
// operation's own coverage, not a replacement for it.

const PAGE_SIZE = 30;

let admin: ApiSession;

test.beforeAll(async () => {
  admin = await authedClient(adminCredentials());
});

/**
 * Posts one bad-credentials attempt straight to the backend's own loopback,
 * bypassing nginx entirely (`docker compose exec certharbor-control`), so the
 * connection's socket peer -- what `clientIP` actually keys the rate limiter
 * on -- is the backend's own IPv6 loopback address, "::1" itself, not
 * whatever address the docker network assigns the container from the test
 * runner's side. A request proxied through certharbor-console would carry a
 * trusted X-Forwarded-For instead, and never exercise "::1" as the literal
 * rate-limiter key this suite needs.
 *
 * The CSRF dance mirrors sessionFetch in helpers/apiClient.ts: GET first to
 * mint the cookie, then POST with it (and the matching header) attached.
 */
async function attemptLoopbackLogin(username: string, password: string): Promise<number> {
  const script = `
    set -e
    jar=$(mktemp)
    curl -sk -c "$jar" -o /dev/null https://[::1]:8443/api/v1/session
    token=$(grep -i certharbor-csrf "$jar" | tail -1 | awk '{print $NF}')
    code=$(curl -sk -b "$jar" -o /dev/null -w '%{http_code}' \
      -X POST https://[::1]:8443/api/v1/session \
      -H "Content-Type: application/json" \
      -H "X-CSRF-Token: $token" \
      --data '{"username":"${username}","password":"${password}"}')
    rm -f "$jar"
    echo "$code"
  `;
  const res = await exec("certharbor-control", "sh", "-c", script);
  expect(res.code, `loopback login attempt script: ${res.stderr}`).toBe(0);
  return Number(res.stdout.trim());
}

test("E2E-SEC-EVENTS-01: security events pagination, search and outcome filter", async () => {
  const before = await admin.call(() => listSecurityEvents({ query: { pageSize: 1 } }));
  expect(before.error, "baseline count request succeeded").toBeUndefined();
  const beforeTotal = before.data?.total ?? 0;

  const tag = Date.now().toString(36);
  const username = `e2e-sec-events-${tag}`;
  const seedCount = PAGE_SIZE + 5 - (beforeTotal % PAGE_SIZE);

  // A bare read never writes auth_log, so this test seeds rows directly
  // against a single disposable username: that guarantees outcome=failure
  // rows it owns and can search for by name, without truncating or
  // otherwise disturbing rows other suites wrote. Seeding through the real
  // login-failure path instead would register every attempt against
  // security.RL (backend/security/security.go): LimitCount is 5 per
  // LimitWindow, so a seed loop of more than 5 would trip the harness's own
  // source IP and 429 every login from it (including this file's own admin
  // session, and every later spec file's) for the next 5 minutes.
  const seeded = await psql(
    `INSERT INTO auth_log (username, ip, success, reason)
     SELECT '${username}', '198.51.100.1', false, 'seeded for E2E-SEC-EVENTS-01'
     FROM generate_series(1, ${seedCount})`,
  );
  expect(seeded, "seeding is a write with no rows returned").toBe("");

  const totalRes = await admin.call(() => listSecurityEvents({ query: { pageSize: 1 } }));
  const total = totalRes.data?.total ?? 0;
  expect(total, "seeding added rows").toBeGreaterThan(beforeTotal);

  const searched = await admin.call(() => listSecurityEvents({ query: { q: username, pageSize: 200 } }));
  expect(searched.error, "search request succeeded").toBeUndefined();
  const items = searched.data?.items ?? [];
  expect(items.length, "the username search matches this test's own rows").toBeGreaterThan(0);
  expect(
    items.every((i: { username?: string }) => i.username === username),
    "no other username survives the search",
  ).toBe(true);
  expect(
    items.every((i: { outcome: string }) => i.outcome === "failure"),
    "every seeded row is a failure",
  ).toBe(true);

  const successOnly = await admin.call(() =>
    listSecurityEvents({ query: { q: username, outcome: "success", pageSize: 200 } }),
  );
  expect(
    successOnly.data?.items ?? [],
    "outcome=success excludes this test's failure rows entirely",
  ).toEqual([]);
});

test("E2E-SEC-EVENTS-02: pageSize over 200 is rejected with 422", async () => {
  const res = await admin.call(() => listSecurityEvents({ query: { pageSize: 201 } }));
  expect(res.response?.status).toBe(422);
});

test("E2E-SEC-EVENTS-03: the endpoint requires an authenticated admin session", async () => {
  const anon = newApiClient();
  const res = await anon.call(() => listSecurityEvents({}));
  expect(res.response?.status, "no cookie, no access").toBe(401);
});

test("E2E-SEC-EVENTS-04: ::1 unblocks end to end -- the rate limiter is actually cleared", async () => {
  test.setTimeout(120_000);

  // Five failed attempts trips security.LimitCount from the loopback's own
  // perspective (clientIP sees "::1" directly, with no trusted-proxy header
  // in the way), the same budget LoginPost enforces for any other IP.
  const username = `e2e-sec-ip-${Date.now().toString(36)}`;
  let lastCode = 0;
  for (let i = 0; i < 5; i++) {
    lastCode = await attemptLoopbackLogin(username, "wrong-on-purpose");
  }
  expect(lastCode, "the 5th bad attempt is still a plain rejection, not yet a block").toBe(401);

  // The 6th attempt is blocked regardless of credentials correctness: this is
  // the rate limiter speaking, not the password check.
  const blockedCode = await attemptLoopbackLogin(username, "wrong-on-purpose");
  expect(blockedCode, "::1 is now rate-limited").toBe(429);

  // Unblock through the generated client, exactly as Section 6's exit
  // criterion specifies: "::1" (not "0:0:0:0:0:0:0:1") is the canonical form
  // deleteIpBlockInput's guard accepts, and encodeURIComponent("::1") is
  // "%3A%3A1" on the wire -- exactly the kind of bug that would silently
  // no-op a delete against the wrong map key instead of erroring.
  const unblock = await admin.call(() => deleteIpBlock({ path: { ip: "::1" } }));
  expect(unblock.error, `unblocking ::1 succeeded: ${JSON.stringify(unblock.error)}`).toBeUndefined();
  expect(unblock.response?.status).toBe(204);

  // The decisive assertion: a 200 from the DELETE call alone would also be
  // produced by a silent no-op against the wrong map key. Only a *cleared*
  // rate limiter lets the very next attempt fail on credentials (401) rather
  // than being rejected by the limiter again (429).
  const afterUnblock = await attemptLoopbackLogin(username, "wrong-on-purpose");
  expect(afterUnblock, "the rate limiter was actually cleared, not just acknowledged").toBe(401);
});

test("E2E-SEC-EVENTS-05: unblocking a non-canonical IP form is rejected with 422", async () => {
  // "0:0:0:0:0:0:0:1" is the same address as "::1" but not its canonical,
  // compressed form -- deleteIPBlockInput's guard must reject it rather than
  // silently clearing nothing.
  const res = await admin.call(() => deleteIpBlock({ path: { ip: "0:0:0:0:0:0:0:1" } }));
  expect(res.response?.status).toBe(422);
});
