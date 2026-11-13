import { psql } from "../../helpers/compose.js";
import { apiLogin, apiMutation, loginOrThrow, logout, newJar } from "../../helpers/session.js";
import { expect, test } from "../../fixtures/test.js";

const PAGE_SIZE = 30;

interface SecurityEvent {
  username?: string;
  outcome: "success" | "failure";
  reason?: string;
}
interface SecurityPage {
  items: SecurityEvent[];
  total: number;
  page: number;
  pageSize: number;
}

test("E2E-SEC-01: security-log pagination, search and filter", async ({ admin }) => {
  test.setTimeout(180_000);
  const username = `sec01-${Date.now().toString(36)}`;
  const password = "Sec01-e2e-Pass1!";
  const create = await admin.send(() => apiMutation(admin.ctx, "POST", "/api/v1/users", { username, password, role: "viewer" }));
  expect(create.status()).toBe(201);

  const jar = await newJar();
  try {
    const denied = await apiLogin(jar, { username, password: "wrong-on-purpose" });
    expect(denied.status()).toBe(401);

    let total = Number(await psql("SELECT count(*) FROM auth_log"));
    while (total < PAGE_SIZE + 1) {
      await loginOrThrow(jar, { username, password });
      const loggedOut = await logout(jar);
      expect(loggedOut.status()).toBe(204);
      total = Number(await psql("SELECT count(*) FROM auth_log"));
    }

    const page1 = await admin.send(() => admin.ctx.get(`/api/v1/security/events?pageSize=${PAGE_SIZE}`));
    expect(page1.status()).toBe(200);
    const first = (await page1.json()) as SecurityPage;
    expect(first.items).toHaveLength(PAGE_SIZE);
    expect(first.total).toBe(total);

    const page2 = await admin.send(() => admin.ctx.get(`/api/v1/security/events?page=2&pageSize=${PAGE_SIZE}`));
    expect(page2.status()).toBe(200);
    const second = (await page2.json()) as SecurityPage;
    expect(second.items).toHaveLength(Math.min(PAGE_SIZE, total - PAGE_SIZE));

    const searched = await admin.send(() => admin.ctx.get(`/api/v1/security/events?q=${username}&pageSize=${PAGE_SIZE}`));
    const searchedPage = (await searched.json()) as SecurityPage;
    expect(searchedPage.items.length, "the username search matches this test's own rows").toBeGreaterThan(0);
    expect(searchedPage.items.every((event) => event.username === username)).toBe(true);

    const ok = await admin.send(() => admin.ctx.get(`/api/v1/security/events?outcome=success&pageSize=${PAGE_SIZE}`));
    const successes = (await ok.json()) as SecurityPage;
    expect(successes.items.length).toBeGreaterThan(0);
    expect(successes.items.every((event) => event.outcome === "success")).toBe(true);

    const fail = await admin.send(() => admin.ctx.get(`/api/v1/security/events?outcome=failure&pageSize=${PAGE_SIZE}`));
    const failures = (await fail.json()) as SecurityPage;
    expect(failures.items.length, "the failure this test produced is here").toBeGreaterThan(0);
    expect(failures.items.every((event) => event.outcome === "failure")).toBe(true);

    const garbage = await admin.send(() => admin.ctx.get("/api/v1/security/events?outcome=garbage"));
    expect(garbage.status()).toBe(422);
  } finally {
    await jar.dispose();
  }
});

test("E2E-SEC-02: audited privileged actions carry the Audit prefix and the right payload", async ({ admin, manager }) => {
  const name = `sec02-${Date.now().toString(36)}`;
  const domain = `${name}.e2e.invalid`;
  const issued = await manager.send(() => apiMutation(manager.ctx, "POST", "/api/v1/certificates", { name, domain, template: "server", keyType: "EC:P-256", duration: "24h" }));
  expect(issued.status()).toBe(201);
  const certificate = (await issued.json()) as { id: number };

  const download = await manager.send(() => manager.ctx.get(`/api/v1/certificates/${certificate.id}/key`));
  expect(download.status(), "the key download that writes the audit row").toBe(200);
  const console_ = await admin.send(() => apiMutation(admin.ctx, "POST", "/api/v1/admin/console/run", { commandId: "app.version" }));
  expect(console_.status()).toBe(200);

  const page = await admin.send(() => admin.ctx.get("/api/v1/security/events?pageSize=200"));
  const body = (await page.json()) as SecurityPage;
  // Both reasons carry the "Audit: " prefix handlers/audit.go's auditPrefix
  // writes (handlers/ca_api.go:178, handlers/admin_console.go:376). The
  // console.run reason also appends command/exit/timeout/duration fields
  // this test does not control, so it is a startsWith match; the key
  // download reason has no such variable suffix, so it is exact.
  expect(body.items.some((event) => event.reason === `Audit: certificate.key_download id=${certificate.id} name=${name} domain=${domain}`)).toBe(true);
  expect(body.items.some((event) => event.reason?.startsWith("Audit: console.run id=app.version"))).toBe(true);
});
