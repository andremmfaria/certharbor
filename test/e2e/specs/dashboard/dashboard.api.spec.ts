import { getDashboard } from "@kantharos-ai/certharbor-client";
import { composeOrThrow, waitHealthy } from "../../helpers/compose.js";
import { ApiSession, authedClient, newApiClient } from "../../helpers/apiClient.js";
import { FIXTURE_USERS } from "../../fixtures/test.js";
import { expect, test } from "@playwright/test";

// GET /api/v1/dashboard (Phase 5 step 3), the aggregate replacing GET / and
// GET /dashboard (D4 decision 8). It composes listCertificates and getCA
// in-process (D10 rule 1); there is no ACME field, since Phase 7 has not
// landed GET /api/v1/acme/* yet (dashboard.go's comment and
// plans/frontend-backend-split.md's Phase 5 step 3 note).

let viewer: ApiSession;

test.beforeAll(async () => {
  viewer = await authedClient(FIXTURE_USERS.viewer);
});

test("E2E-DASH-01: the dashboard aggregates certificate counts and CA status", async () => {
  const res = await viewer.call(() => getDashboard());
  expect(res.error, "GET /api/v1/dashboard succeeded").toBeUndefined();
  const body = res.data;
  expect(body, "a body is returned").toBeTruthy();

  // The total and the byStatus breakdown must agree: this is the same
  // invariant TestAggregateSourceTags enforces statically (every field
  // names its source operation) checked dynamically against live data.
  const counts = body!.certificates;
  const summed = (Object.values(counts.byStatus) as number[]).reduce((a, b) => a + b, 0);
  expect(summed, "byStatus buckets sum to the total").toBe(counts.total);
  expect(counts.expiringSoon, "expiringSoon is exactly the warning bucket").toBe(counts.byStatus.warning ?? 0);
  expect(counts.total, "at least one certificate exists in this environment").toBeGreaterThan(0);

  // CA status, composed from the same data GET /api/v1/ca serves.
  expect(body!.ca.reachable, "the CA is reachable in the healthy case").toBe(true);
  expect(body!.ca.subject.length, "a subject string is present").toBeGreaterThan(0);

  // No ACME field exists on the body yet (deferred to Phase 7).
  expect(body as unknown as Record<string, unknown>, "no acme key is present").not.toHaveProperty("acme");
});

test("E2E-DASH-02: the dashboard still returns 200 when the CA is unreachable", async () => {
  test.setTimeout(240_000);

  await composeOrThrow("stop", "step-ca");
  try {
    const res = await viewer.call(() => getDashboard());
    // FetchCAInfo only returns an error when the root certificate itself
    // fails to parse (a broken install); an unreachable CA server degrades
    // to Reachable=false instead (ca_api.go). The aggregate must not
    // hard-fail just because one composed source reports degraded status.
    expect(res.response?.status, "the aggregate still responds 200").toBe(200);
    const body = res.data;
    expect(body?.ca.reachable, "ca.reachable reflects the outage").toBe(false);
    // The certificate half is unaffected by the CA outage: it is sourced
    // from listCertificates, which never touches the CA server.
    expect(body?.certificates.total, "certificate counts are unaffected by the CA outage").toBeGreaterThan(0);
  } finally {
    await composeOrThrow("start", "step-ca");
    await waitHealthy("step-ca");
  }
});

test("E2E-DASH-03: the dashboard endpoint requires an authenticated session", async () => {
  const anon = newApiClient();
  const res = await anon.call(() => getDashboard());
  expect(res.response?.status, "no cookie, no access").toBe(401);
});
