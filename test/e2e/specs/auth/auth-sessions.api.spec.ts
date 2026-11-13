import { psql } from "../../helpers/compose.js";
import { apiMutation, loginOrThrow, newJar, sessionCookie } from "../../helpers/session.js";
import { AuthedContext, createUser, disposableName, expect, test } from "../../fixtures/test.js";

async function userID(username: string): Promise<string> {
  const id = await psql(`SELECT id FROM users WHERE username = '${username}'`);
  if (!id) throw new Error(`no users row for ${username}`);
  return id;
}

async function adminPatchUser(admin: AuthedContext, id: string, body: Record<string, unknown>): Promise<void> {
  const res = await admin.send(() =>
    apiMutation(admin.ctx, "PATCH", `/api/v1/users/${id}`, body),
  );
  expect(res.status()).toBe(200);
}

test("E2E-AUTH-12: logout revokes a cookie captured before it", async ({ admin }) => {
  const username = disposableName("auth12");
  const password = "Auth12-e2e-Pass1!";
  await createUser(admin, username, password, "admin");
  const jar = await newJar();
  const replay = await newJar();
  try {
    await loginOrThrow(jar, { username, password });
    const captured = await sessionCookie(jar);
    expect((await replay.get("/api/v1/users", { headers: { Cookie: captured } })).status()).toBe(200);

    expect((await apiMutation(jar, "DELETE", "/api/v1/session")).status()).toBe(204);
    const after = await replay.get("/api/v1/users", { headers: { Cookie: captured } });
    expect(after.status(), "a pre-logout copy is revoked too").toBe(401);
    const setCookie = after.headers()["set-cookie"] ?? "";
    expect(setCookie, "the rejected cookie is overwritten").toContain(`${captured.slice(0, captured.indexOf("="))}=`);
  } finally {
    await replay.dispose();
    await jar.dispose();
  }
});

test("E2E-AUTH-14: deactivation, demotion and deletion take effect on the next API request", async ({ admin }) => {
  test.setTimeout(180_000);
  for (const round of ["deactivate", "demote", "delete"] as const) {
    const username = disposableName(`auth14-${round}`);
    const password = "Auth14-e2e-Pass1!";
    await createUser(admin, username, password, "manager");
    const uid = await userID(username);
    const jar = await newJar();
    try {
      await loginOrThrow(jar, { username, password });
      expect((await jar.get("/api/v1/certificates/options")).status()).toBe(200);
      if (round === "deactivate") {
        await adminPatchUser(admin, uid, { isActive: false });
        expect((await jar.get("/api/v1/certificates/options")).status()).toBe(401);
      } else if (round === "demote") {
        await adminPatchUser(admin, uid, { role: "viewer" });
        expect((await jar.get("/api/v1/profile")).status()).toBe(401);
        await loginOrThrow(jar, { username, password });
        expect((await jar.get("/api/v1/profile")).status()).toBe(200);
        expect((await jar.get("/api/v1/certificates/options")).status()).toBe(403);
      } else {
        expect((await apiMutation(admin.ctx, "DELETE", `/api/v1/users/${uid}`)).status()).toBe(204);
        expect((await jar.get("/api/v1/certificates/options")).status(), "deletion rejects rather than panics").toBe(401);
      }
    } finally {
      await jar.dispose();
    }
  }
});

test("E2E-AUTH-15: a password change evicts other sessions but not the acting one", async ({ admin }) => {
  test.setTimeout(180_000);
  const password = "Auth15-e2e-Pass1!";
  const changed = "Auth15-e2e-Changed1!";
  const self = disposableName("auth15-self");
  await createUser(admin, self, password, "viewer");
  const jarA = await newJar();
  const jarB = await newJar();
  try {
    await loginOrThrow(jarA, { username: self, password });
    await loginOrThrow(jarB, { username: self, password });
    const changedPassword = await apiMutation(jarA, "POST", "/api/v1/profile/password", { currentPassword: password, newPassword: changed });
    expect(changedPassword.status()).toBe(204);
    expect((await jarA.get("/api/v1/profile")).status(), "the acting session survives").toBe(200);
    expect((await jarB.get("/api/v1/profile")).status(), "the other session is evicted").toBe(401);
  } finally {
    await jarA.dispose();
    await jarB.dispose();
  }

  const target = disposableName("auth15-reset");
  await createUser(admin, target, password, "viewer");
  const uid = await userID(target);
  const victim = await newJar();
  try {
    await loginOrThrow(victim, { username: target, password });
    const reset = await apiMutation(admin.ctx, "POST", `/api/v1/users/${uid}/password-reset`, { newPassword: changed });
    expect(reset.status()).toBe(200);
    expect((await victim.get("/api/v1/profile")).status(), "an admin reset evicts target sessions").toBe(401);
    const reauth = await newJar();
    try {
      await loginOrThrow(reauth, { username: target, password: changed });
      expect((await reauth.get("/api/v1/profile")).status()).toBe(200);
    } finally { await reauth.dispose(); }
  } finally { await victim.dispose(); }
});
