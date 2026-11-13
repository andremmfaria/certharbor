import { expect, test } from "@playwright/test";
import { BASE_URL } from "../../helpers/env.js";
import { adminCredentials } from "../../fixtures/test.js";

async function signIn(page: import("@playwright/test").Page): Promise<void> {
  const { username, password } = adminCredentials();
  await page.goto(`${BASE_URL}/login`);
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: /Sign In/ }).click();
  await expect(page.getByTestId("page-dashboard")).toBeVisible();
}

test("E2E-AUTH-11: the SPA sign-out control ends the session", async ({ page }) => {
  await signIn(page);
  await page.goto(`${BASE_URL}/admin`);
  await expect(page.getByTestId("page-admin")).toBeVisible();
  await page.getByRole("button", { name: "Sign Out" }).click();
  await expect(page.getByTestId("page-login")).toBeVisible();

  await page.goto(`${BASE_URL}/admin`);
  await expect(page.getByTestId("page-login")).toBeVisible();
});
