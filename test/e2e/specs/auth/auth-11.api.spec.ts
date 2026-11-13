import { apiMutation, csrfToken, loginOrThrow, newJar } from "../../helpers/session.js";
import { createUser, disposableName, expect, test } from "../../fixtures/test.js";

test("E2E-AUTH-11: DELETE /api/v1/session requires CSRF for a live session and ends it", async ({ admin }) => {
  const username = disposableName("auth11");
  const password = "Auth11-e2e-Pass1!";
  await createUser(admin, username, password, "viewer");

  const jar = await newJar();
  try {
    await loginOrThrow(jar, { username, password });
    expect((await jar.get("/api/v1/profile")).status()).toBe(200);

    const noToken = await jar.fetch("/api/v1/session", { method: "DELETE" });
    expect(noToken.status(), "a live session cannot be logged out without its CSRF token").toBe(403);
    expect((await jar.get("/api/v1/profile")).status(), "the rejected request preserves the session").toBe(200);

    const loggedOut = await apiMutation(jar, "DELETE", "/api/v1/session");
    expect(loggedOut.status()).toBe(204);
    const cookies = (await loggedOut.headersArray()).filter((h) => h.name.toLowerCase() === "set-cookie").map((h) => h.value);
    expect(cookies.some((cookie) => /__Host-certharbor(?:-csrf)?=.*Max-Age=0/i.test(cookie))).toBe(true);

    expect((await jar.get("/api/v1/profile")).status(), "the session ended").toBe(401);
  } finally {
    await jar.dispose();
  }
});

test("E2E-AUTH-11: anonymous DELETE /api/v1/session is idempotent", async () => {
  const jar = await newJar();
  try {
    // csrf=optional exists solely so a stale or absent session can be cleared.
    const result = await jar.fetch("/api/v1/session", { method: "DELETE" });
    expect(result.status()).toBe(204);
    expect(((await (await jar.get("/api/v1/session")).json()) as { state: string }).state).toBe("anonymous");
    expect((await csrfToken(jar)).length).toBeGreaterThan(0);
  } finally {
    await jar.dispose();
  }
});
