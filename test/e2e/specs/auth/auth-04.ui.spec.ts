import { expect, test } from "@playwright/test";
import { BASE_URL } from "../../helpers/env.js";
import { decodeQR, secretFromOtpauth } from "../../helpers/qr.js";
import { totpNow } from "../../helpers/totp.js";
import { TWOFA_SUBJECT } from "../../fixtures/test.js";

async function signIn(page: import("@playwright/test").Page, username: string, password: string): Promise<void> {
  await page.goto(`${BASE_URL}/login`);
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: /Sign In/ }).click();
  await expect(page.getByTestId("page-dashboard")).toBeVisible();
}

test("E2E-AUTH-04: the SPA QR image encodes its manual-entry secret", async ({ page }) => {
  const { username, password } = TWOFA_SUBJECT;
  await signIn(page, username, password);
  await page.goto(`${BASE_URL}/profile/2fa`);
  await expect(page.getByTestId("page-profile-mfa")).toBeVisible();

  const qrResponse = page.waitForResponse((response) =>
    response.url().endsWith("/api/v1/profile/mfa/enrollment/qr") && response.status() === 200,
  );
  await page.getByRole("button", { name: "Enable 2FA" }).click();
  const enrollment = page.getByTestId("mfa-enrollment");
  await expect(enrollment).toBeVisible();
  const rendered = await enrollment.getByTestId("mfa-manual-entry-key").textContent();
  expect(rendered, "the SPA renders the pending manual-entry key").toBeTruthy();
  await expect(enrollment.getByAltText("Scan this QR code with your authenticator app")).toBeVisible();

  const png = await qrResponse;
  const encoded = secretFromOtpauth(decodeQR(Buffer.from(await png.body())));
  expect(encoded, "the API QR encodes the secret the SPA renders").toBe(rendered);

  const code = await totpNow(rendered!);
  await enrollment.getByLabel("Code from your authenticator app").fill(code);
  await enrollment.getByRole("button", { name: "Confirm" }).click();
  await expect(page.getByTestId("mfa-disable")).toBeVisible();

  await page.getByLabel("Current password").fill(password);
  await page.getByLabel("Authenticator code").fill(await totpNow(rendered!));
  await page.getByRole("button", { name: "Disable 2FA" }).click();
  await expect(page.getByRole("button", { name: "Enable 2FA" })).toBeVisible();
});
