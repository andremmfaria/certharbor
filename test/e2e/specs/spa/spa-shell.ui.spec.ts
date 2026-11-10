import { createHash } from "node:crypto";
import * as fs from "node:fs";
import * as path from "node:path";
import { Page } from "@playwright/test";
import { composeOrThrow, psql, waitOriginHealthy } from "../../helpers/compose.js";
import { BASE_URL, REPO_ROOT, SPA_BASE } from "../../helpers/env.js";
import { totpNow } from "../../helpers/totp.js";
import { adminCredentials, authed, createUser, disposableName, expect, test } from "../../fixtures/test.js";

// The SPA through the origin the way a browser meets it: HTTPS, so
// SESSION_SECURE's __Host- cookies, nginx's document headers, and the router
// mounted under SPA_BASE.

interface SmokeRoute {
  layout: string;
  name: string;
  smokePath: string;
}

/** frontend/src/routes/routeList.ts, the list the router is built from, read as text so the harness needs no frontend build. */
function routeList(): SmokeRoute[] {
  const src = fs.readFileSync(path.join(REPO_ROOT, "frontend", "src", "routes", "routeList.ts"), "utf8");
  const routes: SmokeRoute[] = [];
  for (const call of src.matchAll(/^\s*entry\((.*)\),\s*$/gm)) {
    const args = [...(call[1] ?? "").matchAll(/'([^']*)'|"([^"]*)"|undefined/g)].map((m) => m[1] ?? m[2] ?? "");
    const [layout, routePath, name, , , smoke] = args;
    if (!layout || !routePath || !name) continue;
    routes.push({ layout, name, smokePath: smoke || routePath });
  }
  expect(routes.length, "routeList.ts parsed into routes").toBeGreaterThan(20);
  return routes;
}

function spa(p: string): string {
  return `${BASE_URL}${SPA_BASE}${p}`;
}

async function signIn(page: Page, username: string, password: string): Promise<void> {
  await page.goto(spa("/login"));
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: /Sign In/ }).click();
}

/** Every Set-Cookie on a response, one entry per header line. */
async function setCookies(res: { headersArray(): Promise<{ name: string; value: string }[]> }): Promise<string[]> {
  return (await res.headersArray()).filter((h) => h.name.toLowerCase() === "set-cookie").map((h) => h.value);
}

/** CSP violations reach the page as securitypolicyviolation events, which a console listener alone can miss. */
async function collectViolations(page: Page): Promise<string[]> {
  const seen: string[] = [];
  page.on("console", (msg) => {
    if (/Content.Security.Policy|Refused to/i.test(msg.text())) seen.push(`console: ${msg.text()}`);
  });
  page.on("pageerror", (err) => seen.push(`pageerror: ${err.message}`));
  await page.addInitScript(() => {
    document.addEventListener("securitypolicyviolation", (e) => {
      console.error(`Content Security Policy violation: ${e.violatedDirective} ${e.blockedURI} at ${e.sourceFile}:${e.lineNumber}:${e.columnNumber}`);
    });
  });
  return seen;
}

test.describe.serial("the SPA through the origin", () => {
  test("E2E-SPA-01: log in at /app/, navigate, hard-refresh a deep link, log out", async ({ page }) => {
    const { username, password } = adminCredentials();

    const loginResponse = page.waitForResponse((r) => r.url().endsWith("/api/v1/session") && r.request().method() === "POST");
    await signIn(page, username, password);
    const login = await loginResponse;
    expect(login.status()).toBe(200);

    // SESSION_SECURE=true over HTTPS: every cookie the login sets carries the
    // __Host- prefix, which browsers accept only with Secure, Path=/ and no
    // Domain, so a wrong attribute would have dropped the cookie silently.
    const cookies = await setCookies(login);
    expect(cookies.length, "the login sets the session and CSRF cookies").toBeGreaterThanOrEqual(2);
    for (const c of cookies) {
      expect(c, "prefixed").toMatch(/^__Host-certharbor(-csrf)?=/);
      expect(c).toMatch(/;\s*Secure/i);
      expect(c).toMatch(/;\s*Path=\/(;|$)/i);
      expect(c).not.toMatch(/;\s*Domain=/i);
    }
    const jar = await page.context().cookies();
    expect(jar.map((c) => c.name).sort()).toEqual(expect.arrayContaining(["__Host-certharbor", "__Host-certharbor-csrf"]));

    // The router writes its root as the bare basename, which nginx redirects to
    // the base with its slash, so a reload there lands on the shell too.
    await expect(page.getByTestId("page-dashboard")).toBeVisible();
    expect(new URL(page.url()).pathname).toMatch(new RegExp(`^${SPA_BASE}/?$`));
    await page.reload();
    await expect(page.getByTestId("page-dashboard")).toBeVisible();

    // Client-side navigation stays under the base.
    await page.getByRole("link", { name: "All Certificates" }).first().click();
    await expect(page.getByTestId("page-certificates")).toBeVisible();
    expect(new URL(page.url()).pathname).toBe(`${SPA_BASE}/certificates`);

    // A cold load of a deep link: nginx's fallback serves the shell and the
    // router resolves the path under its basename.
    await page.goto(spa("/admin/users/1"));
    await expect(page.getByTestId("page-admin-user")).toBeVisible();
    await page.reload();
    await expect(page.getByTestId("page-admin-user")).toBeVisible();

    await page.goto(spa("/certificates"));
    await page.getByRole("button", { name: "Sign Out" }).first().click();
    await expect(page.getByTestId("page-login")).toBeVisible();
    expect(new URL(page.url()).pathname).toBe(`${SPA_BASE}/login`);
    const session = await page.context().request.get(`${BASE_URL}/api/v1/session`);
    expect(((await session.json()) as { state: string }).state).toBe("anonymous");
  });

  test("E2E-SPA-02: a refresh mid-MFA resumes on the code step", async ({ page }) => {
    const admin = await authed(adminCredentials());
    const username = disposableName("spa-mfa");
    const password = "Spa-mfa-e2e-Pass1!";
    // A fixed 20-byte base32 secret, the length handlers/totp.go generates.
    const secret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP";
    try {
      await createUser(admin, username, password, "viewer");
      await psql(`UPDATE users SET totp_enabled = true, totp_secret = '${secret}' WHERE username = '${username}'`);
    } finally {
      await admin.ctx.dispose();
    }

    await signIn(page, username, password);
    await expect(page.getByLabel("2FA Code")).toBeVisible();

    await page.reload();
    await expect(page.getByLabel("2FA Code"), "the code step survives a hard refresh").toBeVisible();
    await expect(page.getByLabel("Password")).toHaveCount(0);

    await page.getByLabel("2FA Code").fill(await totpNow(secret));
    await page.getByRole("button", { name: /Confirm/ }).click();
    await expect(page.getByTestId("page-dashboard")).toBeVisible();
  });

  test("E2E-CSP-01: every route loads under nginx's CSP with no violation", async ({ browser }) => {
    test.setTimeout(5 * 60_000);
    const routes = routeList();

    // Read off the response, not the config: the policy the browser enforces.
    const anonContext = await browser.newContext({ ignoreHTTPSErrors: true });
    const userContext = await browser.newContext({ ignoreHTTPSErrors: true });
    try {
      const probe = await anonContext.request.get(spa("/"));
      const csp = probe.headers()["content-security-policy"] ?? "";
      expect(csp, "nginx sends a document CSP").toContain("default-src 'self'");
      expect(csp).not.toContain("unsafe-inline");
      const html = await probe.text();
      for (const [, attrs, body] of html.matchAll(/<script([^>]*)>([\s\S]*?)<\/script>/g)) {
        if (/\ssrc=/.test(attrs ?? "")) continue;
        const hash = createHash("sha256").update(body ?? "").digest("base64");
        expect(csp, "the inline theme bootstrap is allowed by hash").toContain(`'sha256-${hash}'`);
      }

      const { username, password } = adminCredentials();
      const signedIn = await userContext.newPage();
      await signIn(signedIn, username, password);
      await expect(signedIn.getByTestId("page-dashboard")).toBeVisible();
      await signedIn.close();

      for (const route of routes) {
        const context = route.layout === "auth" ? anonContext : userContext;
        const page = await context.newPage();
        const violations = await collectViolations(page);
        const res = await page.goto(spa(route.smokePath));
        expect(res?.status(), `${route.smokePath} status`).toBe(200);
        expect(res?.headers()["content-security-policy"], `${route.smokePath} carries the CSP`).toBe(csp);
        await expect(page.getByTestId(`page-${route.name}`), `${route.smokePath} renders page-${route.name}`).toBeVisible();
        expect(violations, `${route.smokePath} produced no CSP violation or page error`).toEqual([]);
        await page.close();
      }
    } finally {
      await anonContext.close();
      await userContext.close();
    }
  });

  // Last in the file: it stops the backend, and restores it before returning.
  test("E2E-SPA-03: with the backend stopped the API answers problem+json and the SPA renders the unavailable state", async ({ page }) => {
    test.setTimeout(4 * 60_000);
    await composeOrThrow("stop", "certharbor-control");
    try {
      const res = await page.context().request.get(`${BASE_URL}/api/v1/session`);
      expect(res.status()).toBe(503);
      expect(res.headers()["content-type"]).toBe("application/problem+json");
      expect(((await res.json()) as { type: string }).type).toMatch(/\/upstream-unavailable$/);

      await page.goto(spa("/certificates"));
      await expect(page.getByTestId("unavailable")).toBeVisible();
      await expect(page.getByTestId("page-login")).toHaveCount(0);
      await expect(page.getByLabel("Password")).toHaveCount(0);
    } finally {
      await composeOrThrow("start", "certharbor-control");
      await waitOriginHealthy();
    }
  });
});
