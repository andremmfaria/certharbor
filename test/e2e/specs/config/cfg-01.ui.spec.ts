import * as fs from "node:fs";
import * as path from "node:path";
import { expect, test } from "@playwright/test";
import { BASE_URL, REPO_ROOT } from "../../helpers/env.js";
import { adminCredentials } from "../../fixtures/test.js";

interface Route { layout: string; name: string; smokePath: string }

function spaRoutes(): Route[] {
  const source = fs.readFileSync(path.join(REPO_ROOT, "frontend", "src", "routes", "routeList.ts"), "utf8");
  const routes: Route[] = [];
  for (const call of source.matchAll(/^\s*entry\((.*)\),\s*$/gm)) {
    const values = [...(call[1] ?? "").matchAll(/'([^']*)'|"([^"]*)"|undefined/g)].map((m) => m[1] ?? m[2] ?? "");
    const [layout, routePath, name, , , smoke] = values;
    if (layout === "auth" || !routePath || !name) continue;
    routes.push({ layout: layout!, name: name!, smokePath: smoke || routePath });
  }
  expect(routes.length, "routeList supplies SPA application routes").toBeGreaterThan(20);
  return routes;
}

async function signIn(page: import("@playwright/test").Page): Promise<void> {
  const { username, password } = adminCredentials();
  await page.goto(`${BASE_URL}/login`);
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: /Sign In/ }).click();
  await expect(page.getByTestId("page-dashboard")).toBeVisible();
}

test.describe("E2E-CFG-01: the CSP blocks no current SPA route", () => {
  for (const route of spaRoutes()) {
    test(`${route.smokePath} renders without CSP violations or console errors`, async ({ page }) => {
      const errors: string[] = [];
      await signIn(page);
      page.on("console", (message) => { if (message.type() === "error") errors.push(message.text()); });
      page.on("pageerror", (error) => errors.push(error.message));
      await page.addInitScript(() => {
        (window as unknown as { violations: string[] }).violations = [];
        document.addEventListener("securitypolicyviolation", (event) => {
          (window as unknown as { violations: string[] }).violations.push(`${event.violatedDirective} ${event.blockedURI}`);
        });
      });
      const response = await page.goto(`${BASE_URL}${route.smokePath}`, { waitUntil: "networkidle" });
      expect(response?.status()).toBe(200);
      await expect(page.getByTestId(`page-${route.name}`)).toBeVisible();
      expect(await page.evaluate(() => (window as unknown as { violations: string[] }).violations)).toEqual([]);
      expect(errors).toEqual([]);
    });
  }
});
