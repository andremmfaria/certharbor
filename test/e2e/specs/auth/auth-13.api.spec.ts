import { psql } from "../../helpers/compose.js";
import { FLAGS } from "../../helpers/env.js";
import { mappedGroups, oidcLogin } from "../../helpers/oidc.js";
import { apiMutation, loginOrThrow, newJar } from "../../helpers/session.js";
import { createUser, disposableName, expect, test } from "../../fixtures/test.js";

/**
 * Walks the former V1 escalation chain and asserts it is refused at both of
 * the gates that now close it: the profile rename, and the OIDC upsert's
 * refusal to touch a local row.
 */
test.describe("E2E-AUTH-13: the viewer-to-admin escalation chain is refused at both gates", () => {
  test.skip(!FLAGS.oidc, "E2E_OIDC_ENABLED is unset: no mock IdP on the network");

  test("E2E-AUTH-13: neither the rename nor the upsert escalates a viewer", async ({ admin }) => {
    const groups = mappedGroups();
    const victim = disposableName("victim-viewer");
    const victimPassword = "Auth13-e2e-Pass1!";
    // The OIDC subject the attacker would like to become. Never logged in, so
    // no users row exists for it when the chain starts.
    const collidingName = disposableName("oidc-admin");
    const localPassword = "Auth13-e2e-Local1!";

    await createUser(admin, victim, victimPassword, "viewer");

    // ── Gate one: the profile rename ────────────────────────────────────────
    const viewerJar = await newJar();
    await loginOrThrow(viewerJar, { username: victim, password: victimPassword });

    const renamed = await apiMutation(viewerJar, "PATCH", "/api/v1/profile", { displayName: "Renamed By Test" });
    expect(renamed.status()).toBe(200);
    expect(((await renamed.json()) as { displayName: string }).displayName, "the display name did change").toBe("Renamed By Test");

    // ProfilePost action=update_info reads no username field at all, so the
    // submitted value is ignored rather than rejected. Assert the username is
    // unchanged rather than looking for an error that is never shown.
    const stillNamed = await psql(
      `SELECT count(*) FROM users WHERE username = '${victim}' AND auth_source = 'local'`,
    );
    expect(stillNamed, "the rename did not take: the username is still the original").toBe("1");
    expect(
      await psql(`SELECT count(*) FROM users WHERE username = '${collidingName}'`),
      "no row was created under the attacker's chosen name",
    ).toBe("0");

    // ── Gate two: the OIDC upsert against a local row ───────────────────────
    await createUser(admin, collidingName, localPassword, "viewer");
    const hashBefore = await psql(`SELECT password_hash FROM users WHERE username = '${collidingName}'`);

    const result = await oidcLogin(collidingName, {
      groups: [groups.admin],
      preferred_username: collidingName,
    });
    expect(result.status).toBe(302);
    expect(result.location, "the colliding OIDC login does not log in").toBe("/login?error=denied");

    // UpsertOIDCUser carries WHERE users.auth_source = 'oidc' on both DO UPDATE
    // branches, so the collision updates nothing. A silent promotion is exactly
    // what this test exists to catch, so the role is asserted explicitly rather
    // than inferred from the denial.
    expect(
      await psql(`SELECT role FROM users WHERE username = '${collidingName}'`),
      "the local account keeps its role",
    ).toBe("viewer");
    expect(
      await psql(`SELECT auth_source FROM users WHERE username = '${collidingName}'`),
      "the local account is not converted to an OIDC one",
    ).toBe("local");
    expect(
      await psql(`SELECT password_hash FROM users WHERE username = '${collidingName}'`),
      "the local password hash is untouched",
    ).toBe(hashBefore);

    // The denial is recorded, not merely enacted.
    const security = await admin.send(() => admin.ctx.get("/api/v1/security/events?q=OIDC"));
    expect((await security.text())).toContain("OIDC: username collides with a local account");

    // ── The end of the chain: the local account is still only a viewer ──────
    const localJar = await newJar();
    await loginOrThrow(localJar, { username: collidingName, password: localPassword });
    const adminPage = await localJar.get("/api/v1/users");
    expect(adminPage.status(), "the account reached by local login is still a viewer").toBe(403);
  });
});
