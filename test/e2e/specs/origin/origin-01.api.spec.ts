import * as fs from "node:fs";
import * as path from "node:path";
import { BACKEND_URL, REPO_ROOT, SPA_BASE } from "../../helpers/env";
import { newJar } from "../../helpers/session";
import { expect, test } from "../../fixtures/test";

// PHASE9-DELETE: the server-rendered pages and /static/ leave the origin with
// their template routes, and this file goes with them.
//
// Through the migration window nginx serves the SPA under /app/ and passes
// every other path to the backend. These two tests are what prove the second
// half: a template route or an asset the passthrough lost would come back as
// the SPA shell, an nginx 404 or the @apidown 503, and nothing else in the
// api project requests most of these pages.

/** Every GET the backend registers on its chi router, read off main.go. */
function templateGetRoutes(): string[] {
  const mainGo = fs.readFileSync(path.join(REPO_ROOT, "backend", "main.go"), "utf8");
  const routes = [...mainGo.matchAll(/r\.Get\(\s*"([^"]+)"/g)].map((m) => m[1] ?? "");
  expect(routes.length, "main.go still registers GET routes").toBeGreaterThan(20);
  return routes;
}

// /logout would end the fixture's session, and the OIDC pair 404s unless OIDC
// is enabled, which the main stack is not.
const SKIPPED = new Set(["/logout", "/auth/oidc/login", "/auth/oidc/callback"]);
// The backend's own 404 while the user has no enrolment pending.
const MAY_404 = new Set(["/profile/2fa/qr"]);

test("E2E-ORIGIN-01: every server-rendered route reaches the backend through the published origin", async ({ admin }) => {
  for (const pattern of templateGetRoutes()) {
    if (SKIPPED.has(pattern)) continue;
    const url = pattern.replace(/\{[^}]+\}/g, "999999");
    const res = await admin.send(() => admin.ctx.get(url));
    const body = await res.text();
    const want = url !== pattern || MAY_404.has(pattern) ? [200, 302, 303, 404] : [200, 302, 303];
    expect(want, `GET ${url} through nginx answered ${res.status()}`).toContain(res.status());
    expect(body, `GET ${url} fell back to the SPA shell instead of the backend`).not.toContain('<div id="root"></div>');
    expect(body, `GET ${url} got nginx's upstream-unavailable document`).not.toContain("upstream-unavailable");
  }
});

test("E2E-ORIGIN-01: every /static asset is served through the published origin", async () => {
  const root = path.join(REPO_ROOT, "backend", "static");
  const files: string[] = [];
  const walk = (dir: string): void => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) walk(full);
      else files.push(`/static/${path.relative(root, full).split(path.sep).join("/")}`);
    }
  };
  walk(root);
  expect(files.length, "backend/static carries assets").toBeGreaterThan(0);

  const anon = await newJar();
  try {
    for (const file of files) {
      const res = await anon.get(file);
      expect(res.status(), `GET ${file}`).toBe(200);
      expect(res.headers()["content-type"] ?? "", `Content-Type for ${file}`).not.toBe("");
    }
  } finally {
    await anon.dispose();
  }
});

test("E2E-ORIGIN-01: the SPA is served under its base, by nginx only", async () => {
  const anon = await newJar();
  const direct = await newJar(BACKEND_URL);
  try {
    const shell = await anon.get(`${SPA_BASE}/certificates/1`);
    expect(shell.status()).toBe(200);
    expect(await shell.text(), "an unknown SPA path falls back to the shell").toContain('<div id="root"></div>');
    expect(shell.headers()["cache-control"]).toBe("no-store");

    // The backend itself serves no SPA: the same path straight to it is not
    // the shell, which is what makes the origin the only way to the bundle.
    const bypass = await direct.get(`${SPA_BASE}/certificates/1`);
    expect(await bypass.text()).not.toContain('<div id="root"></div>');
  } finally {
    await anon.dispose();
    await direct.dispose();
  }
});
