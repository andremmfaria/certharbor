import { psqlRows } from "../../helpers/compose.js";
import { apiLogin, apiMutation, csrfToken, logout, newJar } from "../../helpers/session.js";
import { createUser, disposableName, expect, test } from "../../fixtures/test.js";

test("E2E-AUTH-01: successful local login rotates session content", async ({ admin }) => {
  const username = disposableName("auth01");
  const password = "Auth01-e2e-Pass1!";
  await createUser(admin, username, password, "viewer");

  const jar = await newJar();
  try {
    expect((await jar.get("/api/v1/session")).status()).toBe(200);
    const tokenPre = await csrfToken(jar);
    const login = await apiLogin(jar, { username, password });
    expect(login.status()).toBe(200);
    expect((await login.json() as { state: string }).state).toBe("authenticated");

    const tokenPost = await csrfToken(jar);
    expect(tokenPost, "login re-mints the session-bound CSRF token").not.toBe(tokenPre);
    const replay = await apiMutation(jar, "PATCH", "/api/v1/profile/preferences", { theme: "dark" }, tokenPre);
    expect(replay.status(), "the pre-login token no longer validates").toBe(403);
    expect((await replay.json() as { type: string }).type).toMatch(/\/csrf$/);

    const rows = await psqlRows(`SELECT success FROM auth_log WHERE username = $$${username}$$ ORDER BY id DESC LIMIT 1`);
    expect(rows).toEqual(["t"]);
  } finally {
    await logout(jar);
    await jar.dispose();
  }
});
