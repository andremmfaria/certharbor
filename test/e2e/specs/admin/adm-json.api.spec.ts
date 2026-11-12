import {
  getAdminAbout,
  getAdminIntegrity,
  listAdminConsoleCommands,
  listSecurityEvents,
  runAdminConsoleCommand,
} from "@kantharos-ai/certharbor-client";
import { composeOrThrow, waitHealthy } from "../../helpers/compose.js";
import { ApiSession, authedClient, newApiClient } from "../../helpers/apiClient.js";
import { adminCredentials } from "../../fixtures/test.js";
import { expect, test } from "@playwright/test";

// Rewritten against the JSON admin operations (backend/api/admin.go):
// GET /api/v1/admin/about, GET /api/v1/admin/integrity,
// GET /api/v1/admin/console/commands and POST /api/v1/admin/console/run.
// adm-01.api.spec.ts and adm-console.api.spec.ts keep covering the
// still-live template routes (/admin/console, templateRouteRetired=false);
// this file is the new operations' own coverage, not a replacement for it.
//
// The 19-row preflight check-name list and the serial CA-up/CA-down pair
// below are the same calibrated values Section 2.7.5 and E2E-HLTH-06
// record for the template routes; only the transport changed.

let admin: ApiSession;

test.beforeAll(async () => {
  admin = await authedClient(adminCredentials());
});

// Serial and adjacent, same pairing rationale as adm-console.api.spec.ts's
// ADM-02/ADM-03: E2E-HLTH-06 below alone passes against a stubbed Health()
// that always returns nil, and the CA-down half is what makes it meaningful.
test.describe.configure({ mode: "serial" });

const PREFLIGHT_CHECK_NAMES = [
  "PostgreSQL",
  "Step-CA API",
  "Root CA certificate",
  "Intermediate CA certificate",
  "Provisioner password file",
  "UI TLS certificate",
  "UI TLS private key",
  "Issued certificates directory",
  "Upload directory",
  "CA config",
  "Root CA integrity",
  "Intermediate CA integrity",
  "Full chain",
  "Provisioner password sync",
  "step-ca image pin",
  "Disk space",
  "Disk space",
  "Disk space",
  "Session cookie",
];

test("E2E-ADM-01 (JSON): app.version pins the certificates library", async () => {
  const commands = await admin.call(() => listAdminConsoleCommands({}));
  expect(commands.error, "command list request succeeded").toBeUndefined();
  const ids = (commands.data ?? []).map((c: { id: string }) => c.id);
  expect(ids, "app.version is offered as a native entry").toContain("app.version");
  expect(ids, "ca.health is offered as a native entry").toContain("ca.health");

  const run = await admin.call(() => runAdminConsoleCommand({ body: { commandId: "app.version" } }));
  expect(run.error, "app.version ran").toBeUndefined();
  expect(run.data?.success).toBe(true);
  expect(run.data?.exitCode).toBe(0);

  // The second line only, exactly as adm-01.api.spec.ts asserts against the
  // template route: the first line's Version/BuildDate/GitCommit are
  // compile-time defaults with no -X ldflags set.
  const lines = (run.data?.output ?? "").split("\n");
  expect(lines[1], "Risk R3: the pinned certificates library in the built image").toMatch(
    /smallstep\/certificates v0\.30\.2/,
  );

  const security = await admin.call(() => listSecurityEvents({ query: { pageSize: 200 } }));
  // backend/api/security.go's Reason field is `omitempty`: a success event
  // with no reason (e.g. a plain login) drops the key entirely, so it comes
  // back here as undefined, not "". Default to "" so `.some` can scan the
  // whole page (including a negative assertion that must reach the end
  // without a match) without throwing on those rows.
  const reasons = (security.data?.items ?? []).map((i: { reason?: string }) => i.reason ?? "");
  expect(
    reasons.some((r: string) => r.includes("console.run id=app.version") && /duration=\S+/.test(r)),
    "the audit row is the black-box shadow of the allowlist invariant",
  ).toBe(true);
});

test("E2E-ADM-05 (JSON): an unknown command id is rejected with 422 and audited, not run", async () => {
  const run = await admin.call(() => runAdminConsoleCommand({ body: { commandId: "rm.rf" } }));
  expect(run.response?.status).toBe(422);

  const security = await admin.call(() => listSecurityEvents({ query: { pageSize: 200 } }));
  // Same omitempty/undefined-reason caveat as E2E-ADM-01 above.
  const reasons = (security.data?.items ?? []).map((i: { reason?: string }) => i.reason ?? "");
  expect(
    reasons.some((r: string) => r.includes("console.denied command_id=rm.rf")),
    "the denial is audited",
  ).toBe(true);
  expect(
    reasons.some((r: string) => r.includes("console.run id=rm.rf")),
    "no console.run row for a command that never ran",
  ).toBe(false);
});

test("E2E-ADM-06 (JSON): the console allowlist is a fixed, small set", async () => {
  const commands = await admin.call(() => listAdminConsoleCommands({}));
  expect(commands.error).toBeUndefined();
  const items = commands.data ?? [];
  // A unit assertion over the slice itself, not a count of rendered <option>
  // elements (plans/e2e-tests.md's note on E2E-ADM-06).
  expect(items.length).toBeGreaterThan(0);
  expect(items.length).toBeLessThanOrEqual(16);
  for (const c of items as { id: string; label: string; description: string }[]) {
    expect(c.id.length).toBeGreaterThan(0);
    expect(c.label.length).toBeGreaterThan(0);
    // Name/Args/NativeFn never reach the wire (handlers/admin_console.go) --
    // the DTO carries only id, label and description.
    expect(Object.keys(c).sort()).toEqual(["description", "id", "label"]);
  }
});

test("E2E-ADM-07 (JSON): the /admin/about preflight check list, ordered", async () => {
  const about = await admin.call(() => getAdminAbout());
  expect(about.error).toBeUndefined();
  const names = (about.data?.checks ?? []).map((c: { name: string }) => c.name);
  expect(names, "the ordered check-name list matches the Section 2.7.5 calibration").toEqual(
    PREFLIGHT_CHECK_NAMES,
  );
});

test("E2E-ADM-08 (JSON): the admin JSON operations require an authenticated admin session", async () => {
  const anon = newApiClient();

  const about = await anon.call(() => getAdminAbout());
  expect(about.response?.status, "no cookie, no access").toBe(401);

  const integrity = await anon.call(() => getAdminIntegrity());
  expect(integrity.response?.status, "no cookie, no access").toBe(401);

  const commands = await anon.call(() => listAdminConsoleCommands({}));
  expect(commands.response?.status, "no cookie, no access").toBe(401);
});

test("E2E-HLTH-06 (JSON): /admin/integrity tracks live CA availability and nothing else", async () => {
  const up = await admin.call(() => getAdminIntegrity());
  expect(up.error).toBeUndefined();
  const upByName = new Map<string, { status: string; detail: string }>(
    (up.data?.checks ?? []).map(
      (c: { name: string; status: string; detail: string }): [string, { status: string; detail: string }] => [
        c.name,
        { status: c.status, detail: c.detail },
      ],
    ),
  );

  test.setTimeout(300_000);
  await composeOrThrow("stop", "step-ca");
  try {
    const down = await admin.call(() => getAdminIntegrity());
    expect(down.error).toBeUndefined();
    const downByName = new Map<string, { status: string; detail: string }>(
      (down.data?.checks ?? []).map(
        (c: { name: string; status: string; detail: string }): [string, { status: string; detail: string }] => [
          c.name,
          { status: c.status, detail: c.detail },
        ],
      ),
    );

    expect(
      [...downByName.keys()].sort(),
      "stopping the CA changes no check's name, only some statuses",
    ).toEqual([...upByName.keys()].sort());

    const changed = [...downByName.keys()].filter(
      (name) => downByName.get(name)?.status !== upByName.get(name)?.status,
    );
    expect(changed.length, "at least the CA-reachability check regresses").toBeGreaterThan(0);
  } finally {
    await composeOrThrow("start", "step-ca");
    await waitHealthy("step-ca");
  }
});
