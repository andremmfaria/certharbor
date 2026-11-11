import { listAuditEvents } from "@kantharos-ai/certharbor-client";
import { psql } from "../../helpers/compose.js";
import { ApiSession, authedClient, newApiClient } from "../../helpers/apiClient.js";
import { adminCredentials } from "../../fixtures/test.js";
import { expect, test } from "@playwright/test";

// Rewritten against GET /api/v1/audit-events through the generated client
// (clients/ts), the same pipeline hist.api.spec.ts was rewritten against.
// Unlike /api/v1/security/events, this operation takes no q/outcome filter
// (backend/api/security.go's listAuditEventsInput carries only page/
// pageSize), so this suite covers pagination and the auth gate only.

const PAGE_SIZE = 30;

let admin: ApiSession;

test.beforeAll(async () => {
  admin = await authedClient(adminCredentials());
});

test("E2E-AUDIT-01: audit events pagination via GET /api/v1/audit-events", async () => {
  const before = await admin.call(() => listAuditEvents({ query: { pageSize: 1 } }));
  expect(before.error, "baseline count request succeeded").toBeUndefined();
  const beforeTotal = before.data?.total ?? 0;

  // A bare read never writes auth_log, so this test seeds rows directly
  // (mirroring hist.api.spec.ts's cert_history seed), enough to guarantee a
  // second page exists. Seeding through the real login-failure path instead
  // would register every attempt against security.RL (backend/security/
  // security.go): LimitCount is 5 per LimitWindow, so a seed loop of more
  // than 5 would trip the harness's own source IP and 429 every login from
  // it (including this file's own admin session) for the next 5 minutes.
  const tag = Date.now().toString(36);
  const username = `e2e-audit-${tag}`;
  const seedCount = PAGE_SIZE + 5 - (beforeTotal % PAGE_SIZE);

  const seeded = await psql(
    `INSERT INTO auth_log (username, ip, success, reason)
     SELECT '${username}', '198.51.100.1', false, 'seeded for E2E-AUDIT-01'
     FROM generate_series(1, ${seedCount})`,
  );
  expect(seeded, "seeding is a write, not a SELECT: psql's -At still prints the INSERT command tag").toMatch(
    /^INSERT 0 \d+$/,
  );

  const totalRes = await admin.call(() => listAuditEvents({ query: { pageSize: 1 } }));
  const total = totalRes.data?.total ?? 0;
  expect(total, "seeding added rows").toBeGreaterThan(beforeTotal);

  const expectedPages = Math.ceil(total / PAGE_SIZE);
  expect(expectedPages, "more than one page exists, so page 2 is meaningful").toBeGreaterThan(1);

  const page1 = await admin.call(() => listAuditEvents({ query: { page: 1, pageSize: PAGE_SIZE } }));
  expect(page1.error, "page 1 request succeeded").toBeUndefined();
  const items1 = page1.data?.items ?? [];
  expect(items1.length, "page 1 is full").toBe(PAGE_SIZE);

  const page2 = await admin.call(() => listAuditEvents({ query: { page: 2, pageSize: PAGE_SIZE } }));
  const items2 = page2.data?.items ?? [];
  const expectedOnPage2 = Math.min(PAGE_SIZE, total - PAGE_SIZE);
  expect(items2.length, `page 2 holds the remainder of ${total} rows`).toBe(expectedOnPage2);

  // The two pages must be disjoint: a page parameter that is silently
  // ignored returns a full, identical first page and would pass a
  // count-only check.
  const ids1 = items1.map((i: { id: number }) => i.id);
  const ids2 = items2.map((i: { id: number }) => i.id);
  const overlap = ids1.filter((id: number) => ids2.includes(id));
  expect(overlap, "page 1 and page 2 share no entries").toEqual([]);

  // This test's own seeded failures are visible in the unfiltered feed,
  // proving the operation reads the same auth_log rows listSecurityEvents
  // does rather than a separate, empty table.
  const all = await admin.call(() => listAuditEvents({ query: { pageSize: 200 } }));
  const usernames = (all.data?.items ?? []).map((i: { username?: string }) => i.username);
  expect(usernames, "the unfiltered feed carries this test's own seeded rows").toContain(username);
});

test("E2E-AUDIT-02: pageSize over 200 is rejected with 422", async () => {
  const res = await admin.call(() => listAuditEvents({ query: { pageSize: 201 } }));
  expect(res.response?.status).toBe(422);
});

test("E2E-AUDIT-03: the endpoint requires an authenticated admin session", async () => {
  const anon = newApiClient();
  const res = await anon.call(() => listAuditEvents({}));
  expect(res.response?.status, "no cookie, no access").toBe(401);
});
