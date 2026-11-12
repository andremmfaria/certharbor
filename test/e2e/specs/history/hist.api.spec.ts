import { createCertificate, listHistory, renewCertificate } from "@kantharos-ai/certharbor-client";
import { compose } from "../../helpers/compose.js";
import { ApiSession, authedClient } from "../../helpers/apiClient.js";
import { adminCredentials } from "../../fixtures/test.js";
import { expect, test } from "@playwright/test";

// Rewritten against GET /api/v1/history through the generated client
// (clients/ts), the same pipeline cert-01.api.spec.ts was rewritten against.
// The pre-split assertions scraped <span class="badge ...">action</span>
// out of server-rendered HTML; the new operation returns a typed
// PageHistoryEntryBody instead, so the row/action/filter checks below read
// JSON fields (item.action, item.certName) rather than matching markup.

const PAGE_SIZE = 30;

let admin: ApiSession;

test.beforeAll(async () => {
  admin = await authedClient(adminCredentials());
});

test("E2E-HIST-01: history pagination via GET /api/v1/history", async () => {
  // The total is computed, never assumed: other suites (certificates,
  // provisioners) write history rows of their own, so the table is seeded
  // rather than truncated.
  const before = await admin.call(() => listHistory({ query: { pageSize: 1 } }));
  expect(before.error, "baseline count request succeeded").toBeUndefined();
  const beforeTotal = before.data?.total ?? 0;

  // The tag makes seeded names unique per run: a bare index would look like
  // an overlap between pages on a re-run.
  const tag = Date.now().toString(36);
  const seed = PAGE_SIZE + 5 - (beforeTotal % PAGE_SIZE);
  const seeded = await compose(
    "exec",
    "-T",
    "postgres",
    "psql",
    "-v",
    "ON_ERROR_STOP=1",
    "-U",
    "certharbor",
    "-d",
    "certharbor",
    "-c",
    `INSERT INTO cert_history (action, cert_name, domain, details, username, role)
     SELECT 'issue', 'e2e-seed-${tag}-' || g, 'seed' || g || '.e2e.invalid', 'synthetic', 'e2e-seed', 'admin'
     FROM generate_series(1, ${seed}) AS g;`,
  );
  expect(seeded.code, `seeding ${seed} rows: ${seeded.stderr}`).toBe(0);

  const totalRes = await admin.call(() => listHistory({ query: { pageSize: 1 } }));
  const total = totalRes.data?.total ?? 0;
  const expectedPages = Math.ceil(total / PAGE_SIZE);
  expect(expectedPages, "more than one page exists, so page 2 is meaningful").toBeGreaterThan(1);

  const page1 = await admin.call(() => listHistory({ query: { page: 1, pageSize: PAGE_SIZE } }));
  expect(page1.error, "page 1 request succeeded").toBeUndefined();
  const items1 = page1.data?.items ?? [];
  expect(items1.length, "page 1 is full").toBe(PAGE_SIZE);

  const page2 = await admin.call(() => listHistory({ query: { page: 2, pageSize: PAGE_SIZE } }));
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
});

test("E2E-HIST-02: the history action filter", async () => {
  const filtered = await admin.call(() => listHistory({ query: { action: ["issue", "revoke"] } }));
  expect(filtered.error, "filtered request succeeded").toBeUndefined();
  const filteredActions = (filtered.data?.items ?? []).map((i: { action: string }) => i.action);

  // Scoped by certificate name rather than left wholly unfiltered, mirroring
  // the server-rendered era's control: this proves renew rows exist at all
  // in this environment, so their absence above is not simply "no renews
  // happened yet". "e2e-dur-8760h" (cert-01's duration-axis fixture) used to
  // be the stand-in here, but that cert is only ever issued, never renewed,
  // by any spec in this suite — a stale reference from before cert-04's
  // renew/revoke subjects were made per-run-unique to stop interfering with
  // each other (see cert-04.api.spec.ts's own comment on that). Renewing a
  // cert of this test's own here makes the control self-contained instead of
  // depending on another file's side effect and run order.
  const controlSubject = `e2e-hist-control-${Date.now().toString(36)}`;
  const controlIssued = await admin.call(() =>
    createCertificate({
      body: { name: controlSubject, domain: `${controlSubject}.example.com`, template: "server", keyType: "EC:P-256", duration: "8760h" },
    }),
  );
  expect(controlIssued.response?.status, "control precondition issuance").toBe(201);
  const controlRenew = await admin.call(() => renewCertificate({ path: { id: controlIssued.data!.id } }));
  expect(controlRenew.response?.status, "control renew").toBe(200);

  const control = await admin.call(() => listHistory({ query: { cert: controlSubject } }));
  const controlActions = (control.data?.items ?? []).map((i: { action: string }) => i.action);

  expect(filteredActions.length, "zero rows would satisfy the exclusion trivially").toBeGreaterThan(0);
  expect(filteredActions.filter((a: string) => a === "renew"), "no renew survives the filter").toEqual([]);
  // Without this third clause the exclusion is unfalsifiable if the action
  // filter were silently ignored by the query layer.
  expect(controlActions, "a renew row is visible when no action filter is applied").toContain("renew");
});

test("E2E-HIST-03: the history certificate-name filter", async () => {
  const subject = "e2e-server-ec-p256";

  const filtered = await admin.call(() => listHistory({ query: { cert: subject } }));
  expect(filtered.error, "filtered request succeeded").toBeUndefined();
  const items = filtered.data?.items ?? [];
  const names = items.map((i: { certName: string }) => i.certName);

  expect(items.length, "the filter returns something").toBeGreaterThan(0);
  expect(names.every((n: string) => n === subject), "no other certificate survives").toBe(true);

  const unfiltered = await admin.call(() => listHistory({ query: { pageSize: 200 } }));
  const unfilteredNames = (unfiltered.data?.items ?? []).map((i: { certName: string }) => i.certName);
  expect(
    unfilteredNames.some((n: string) => n.startsWith("e2e-seed-") || /e2e-(dur|client|wildcard|internal)/.test(n)),
    "the unfiltered view carries entries for a different certificate",
  ).toBe(true);
});

test("E2E-HIST-04: pageSize over 200 is rejected with 422", async () => {
  const res = await admin.call(() => listHistory({ query: { pageSize: 201 } }));
  expect(res.response?.status).toBe(422);
});

test("E2E-HIST-05: page*pageSize beyond the max offset is rejected with 422", async () => {
  const res = await admin.call(() => listHistory({ query: { page: 51, pageSize: 200 } }));
  expect(res.response?.status).toBe(422);
});
