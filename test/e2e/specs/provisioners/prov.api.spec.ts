import { listProvisioners } from "@kantharos-ai/certharbor-client";
import { composeOrThrow, exec, waitHealthy } from "../../helpers/compose.js";
import { ApiSession, authedClient, newApiClient } from "../../helpers/apiClient.js";
import { FIXTURE_USERS } from "../../fixtures/test.js";
import { expect, test } from "@playwright/test";

// Rewritten against GET /api/v1/provisioners through the generated client
// (clients/ts), the same pipeline cert-01.api.spec.ts was rewritten against.
// The pre-split assertions scraped <div class="prov-card"> out of
// server-rendered HTML; the new operation returns Array<ProvisionerBody> |
// null instead, so the row count and CA-degrade checks below read the JSON
// array length rather than matching markup.

let viewer: ApiSession;

// Oracle pair: each half passes against a stub the other would catch, so
// flake triage must never retire one without the other.
let rowsWhenHealthy = 0;

test.beforeAll(async () => {
  viewer = await authedClient(FIXTURE_USERS.viewer);
});

test("E2E-PROV-01: the provisioner list matches the CA's own configuration", async () => {
  // jq ships in the step-ca image, which is what step-ca-bootstrap.sh relies on.
  // There is no `step-ca provisioner list` subcommand.
  const caJSON = await exec("step-ca", "sh", "-c", "cat /home/step/config/ca.json | jq -c '.authority.provisioners'");
  expect(caJSON.code, `reading ca.json: ${caJSON.stderr}`).toBe(0);
  const provisioners = JSON.parse(caJSON.stdout.trim()) as Array<{ name: string; type: string }>;
  expect(provisioners.length, "the CA has at least one provisioner").toBeGreaterThan(0);

  const res = await viewer.call(() => listProvisioners({}));
  expect(res.error, "GET /api/v1/provisioners succeeded").toBeUndefined();
  const body = res.data ?? [];

  for (const p of provisioners) {
    const match = body.find((item: { name: string; type: string }) => item.name === p.name);
    expect(match, `provisioner ${p.name} is listed`).toBeTruthy();
    expect(match?.type, `provisioner ${p.name} has type ${p.type}`).toBe(p.type);
  }

  rowsWhenHealthy = body.length;
  expect(rowsWhenHealthy, "the healthy response lists at least one provisioner").toBeGreaterThan(0);
});

test("E2E-PROV-02: the response degrades rather than failing when the CA is unreachable", async () => {
  test.setTimeout(240_000);
  expect(rowsWhenHealthy, "E2E-PROV-01 must run first: it is this test's positive control").toBeGreaterThan(0);

  await composeOrThrow("stop", "step-ca");
  try {
    const res = await viewer.call(() => listProvisioners({}));
    // Section 5's CA-error-degrade contract: a provisioner-list failure
    // degrades to an empty/null body, never a 5xx.
    expect(res.response?.status, "degrades, never 5xx").toBeLessThan(500);
    const body = res.data ?? [];
    expect(body.length, `row count fell from ${rowsWhenHealthy} to zero`).toBe(0);
  } finally {
    await composeOrThrow("start", "step-ca");
    await waitHealthy("step-ca");
  }
});

test("E2E-PROV-03: provisioners endpoint requires an authenticated session", async () => {
  const anon = newApiClient();
  const res = await anon.call(() => listProvisioners({}));
  expect(res.response?.status, "no cookie, no access").toBe(401);
});
