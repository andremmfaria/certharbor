import { Page } from "@playwright/test";
import { BASE_URL } from "../../helpers/env.js";
import { decodeQR, secretFromOtpauth } from "../../helpers/qr.js";
import { totpNow, totpWrong } from "../../helpers/totp.js";
import { createUser, disposableName, expect, test } from "../../fixtures/test.js";

// Each test owns a fresh, disposable user (Section 4.1.4's fixture contract):
// MFA enrollment state is sticky per account, so sharing `viewer`/`manager`
// here would leak enabled-2FA state into every other spec that signs in as
// them. `twofa_user` (fixtures/test.ts's TWOFA_SUBJECT) is deliberately not
// reused either — see auth-04.ui.spec.ts's note below.

async function signIn(page: Page, username: string, password: string): Promise<void> {
  await page.goto(`${BASE_URL}/login`);
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: /Sign In/ }).click();
  await expect(page.getByTestId("page-dashboard")).toBeVisible();
}

/**
 * Starts enrollment through the UI and decodes the rendered QR against the
 * manual-entry key next to it (the same cross-check E2E-AUTH-04 established),
 * returning the secret so the caller can compute further TOTP codes of its
 * own without re-deriving it.
 */
async function startEnrollmentViaUi(page: Page): Promise<string> {
  await page.goto(`${BASE_URL}/profile/2fa`);
  await expect(page.getByTestId("page-profile-mfa")).toBeVisible();

  const qrResponse = page.waitForResponse(
    (r) => r.url().endsWith("/api/v1/profile/mfa/enrollment/qr") && r.status() === 200,
  );
  await page.getByRole("button", { name: "Enable 2FA" }).click();
  const enrollment = page.getByTestId("mfa-enrollment");
  await expect(enrollment).toBeVisible();

  const rendered = await enrollment.getByTestId("mfa-manual-entry-key").textContent();
  expect(rendered, "the SPA renders the pending manual-entry key").toBeTruthy();

  const png = await qrResponse;
  const encoded = secretFromOtpauth(decodeQR(Buffer.from(await png.body())));
  expect(encoded, "the QR encodes the same secret the page renders next to it").toBe(rendered);

  return rendered!;
}

/** Drives the confirm step with a valid code and dismisses the recovery-codes step, landing on the disable form. */
async function confirmEnrollmentViaUi(page: Page, secret: string): Promise<void> {
  const enrollment = page.getByTestId("mfa-enrollment");
  await enrollment.getByLabel("Code from your authenticator app").fill(await totpNow(secret));
  await enrollment.getByRole("button", { name: "Confirm" }).click();
  await expect(page.getByTestId("mfa-recovery-codes")).toBeVisible();
  await page.getByRole("button", { name: "I've saved these codes" }).click();
  await expect(page.getByTestId("mfa-disable")).toBeVisible();
}

test("E2E-MFA-01: enroll, confirm, see recovery codes once, then disable with dual reauth", async ({
  page,
  admin,
}) => {
  const username = disposableName("mfa-happy");
  const password = "Mfa-e2e-happy-Pass1!";
  await createUser(admin, username, password, "viewer");

  await signIn(page, username, password);
  const secret = await startEnrollmentViaUi(page);

  const enrollment = page.getByTestId("mfa-enrollment");
  await enrollment.getByLabel("Code from your authenticator app").fill(await totpNow(secret));
  await enrollment.getByRole("button", { name: "Confirm" }).click();

  // Recovery codes are the response's own payload (confirmMfaEnrollment's
  // writeOnly RecoveryCodes field): shown exactly once, here.
  const recovery = page.getByTestId("mfa-recovery-codes");
  await expect(recovery).toBeVisible();
  const codes = await recovery.locator(".recovery-codes li code").allTextContents();
  expect(codes, "8 one-time recovery codes, each matching generateRecoveryCodes(8)'s format").toHaveLength(8);
  for (const code of codes) {
    expect(code).toMatch(/^[A-Za-z0-9]{6}-[A-Za-z0-9]{6}-[A-Za-z0-9]{6}$/);
  }
  expect(new Set(codes).size, "all 8 codes are distinct").toBe(8);

  await recovery.getByRole("button", { name: "I've saved these codes" }).click();

  // MFA now shows enabled, and the recovery codes are gone from the page:
  // dismissing is a one-way local action (nothing left on the server to
  // re-fetch them from), not a reload-survives-it cache.
  await expect(page.getByText("Two-factor authentication is enabled on this account.")).toBeVisible();
  await expect(page.getByTestId("mfa-disable")).toBeVisible();
  await expect(page.getByTestId("mfa-recovery-codes")).toHaveCount(0);
  await page.reload();
  await expect(page.getByTestId("mfa-recovery-codes")).toHaveCount(0);
  await expect(page.getByTestId("mfa-disable")).toBeVisible();

  // Disable requires both the current password and a fresh TOTP code.
  await page.getByLabel("Current password").fill(password);
  await page.getByLabel("Authenticator code").fill(await totpNow(secret));
  await page.getByRole("button", { name: "Disable 2FA" }).click();

  await expect(page.getByRole("button", { name: "Enable 2FA" })).toBeVisible();
  await expect(page.getByTestId("mfa-disable")).toHaveCount(0);
});

test("E2E-MFA-02: a wrong confirmation code is refused, leaves enrollment pending, shows no recovery codes", async ({
  page,
  admin,
}) => {
  const username = disposableName("mfa-wrongcode");
  const password = "Mfa-e2e-wrongcode-Pass1!";
  await createUser(admin, username, password, "viewer");

  await signIn(page, username, password);
  const secret = await startEnrollmentViaUi(page);

  const enrollment = page.getByTestId("mfa-enrollment");
  await enrollment.getByLabel("Code from your authenticator app").fill(await totpWrong(secret));
  await enrollment.getByRole("button", { name: "Confirm" }).click();

  await expect(enrollment.getByTestId("mfa-confirm-error")).toHaveText("That code was not accepted.");
  await expect(page.getByTestId("mfa-recovery-codes")).toHaveCount(0);
  // Still on the confirm step: enrollment was not completed, and the code
  // field is cleared rather than left holding the rejected value.
  await expect(enrollment).toBeVisible();
  await expect(enrollment.getByLabel("Code from your authenticator app")).toHaveValue("");
});

test("E2E-MFA-03: disabling with the correct password but a wrong code is refused, MFA stays enabled", async ({
  page,
  admin,
}) => {
  const username = disposableName("mfa-wrongdis-code");
  const password = "Mfa-e2e-wrongdiscode-Pass1!";
  await createUser(admin, username, password, "viewer");

  await signIn(page, username, password);
  const secret = await startEnrollmentViaUi(page);
  await confirmEnrollmentViaUi(page, secret);

  await page.getByLabel("Current password").fill(password);
  await page.getByLabel("Authenticator code").fill(await totpWrong(secret));
  await page.getByRole("button", { name: "Disable 2FA" }).click();

  await expect(page.getByTestId("mfa-disable-error")).toHaveText("That authenticator code was not accepted.");
  await expect(page.getByTestId("mfa-disable")).toBeVisible();
  await expect(page.getByRole("button", { name: "Enable 2FA" })).toHaveCount(0);
});

test("E2E-MFA-04: disabling with a wrong password but a fresh code is refused, MFA stays enabled", async ({
  page,
  admin,
}) => {
  const username = disposableName("mfa-wrongdis-pass");
  const password = "Mfa-e2e-wrongdispass-Pass1!";
  await createUser(admin, username, password, "viewer");

  await signIn(page, username, password);
  const secret = await startEnrollmentViaUi(page);
  await confirmEnrollmentViaUi(page, secret);

  await page.getByLabel("Current password").fill("not-the-password");
  await page.getByLabel("Authenticator code").fill(await totpNow(secret));
  await page.getByRole("button", { name: "Disable 2FA" }).click();

  await expect(page.getByTestId("mfa-disable-error")).toHaveText("Current password is incorrect.");
  await expect(page.getByTestId("mfa-disable")).toBeVisible();
  await expect(page.getByRole("button", { name: "Enable 2FA" })).toHaveCount(0);
});
