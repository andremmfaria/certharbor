import { APIRequestContext, APIResponse } from "@playwright/test";
import { GoldenRoute, goldenRoutes, withID } from "../../helpers/routes.js";
import { apiMutation, newJar } from "../../helpers/session.js";
import { AuthedContext, disposableName, expect, test } from "../../fixtures/test.js";

// Rewritten against the API role matrix. The pre-split table scraped
// server-rendered pages (`<div class="prov-card">`-style content strings)
// behind main.go's chi router; Phase 9 deleted every one of those routes, so
// the matrix below is driven entirely from backend/testdata/roles.golden
// (the same source api/roles_test.go checks the live router against) rather
// than from main.go.
//
// backend/api/middleware.go's chain order is session -> role -> csrf -> rate
// limit (api.go:109-116), and roleMiddleware answers a role failure with a
// generic 403 "insufficient role" problem document *before* csrfMiddleware
// ever runs. That ordering is what the mutation half of this matrix below
// exploits: an insufficient-role mutation is refused without ever presenting
// a CSRF token, so the role gate can be asserted in isolation from the CSRF
// gate E2E-CSRF-01 already owns. Sufficient-role mutation *success* is
// intentionally left to each feature's own spec (adm-01, cert-*, prov, hist)
// and to E2E-CSRF-01's own token sweep, rather than re-proving it here a
// fourth time with its own disposable state.

type RoleName = "viewer" | "manager" | "admin";
const ROLE_LEVEL: Record<RoleName, number> = { viewer: 1, manager: 2, admin: 3 };
const ROLES: RoleName[] = ["viewer", "manager", "admin"];

function sufficient(role: RoleName, route: GoldenRoute): boolean {
  return ROLE_LEVEL[role] >= ROLE_LEVEL[route.role as RoleName];
}

function send(ctx: APIRequestContext, route: GoldenRoute, url: string): Promise<APIResponse> {
  if (route.method === "GET") return ctx.get(url);
  return ctx.fetch(url, { method: route.method, data: {} });
}

async function expectInsufficientRole(role: RoleName, route: GoldenRoute, res: APIResponse): Promise<void> {
  const where = `${role} ${route.method} ${route.pattern}`;
  expect(res.status(), `${where} status`).toBe(403);
  expect(res.headers()["content-type"] ?? "", `${where} content type`).toContain("application/problem+json");
  const body = (await res.json()) as { title?: string; detail?: string };
  expect(body.title, `${where} problem title`).toBe("Forbidden");
  expect(body.detail, `${where} problem detail`).toBe("insufficient role");
}

const REQUIRED_ROUTES = goldenRoutes().filter((route) => route.auth === "required");

// A GET route carrying a {id}-style placeholder 404s on a nonexistent id
// regardless of role, which would make a bare 200/403 assertion ambiguous.
// The static (placeholder-free) GETs below are the ones where 200 vs 403 is
// an unambiguous role-gate signal; the id-bearing boundary case
// (/certificates/{id} vs .../key) gets its own dedicated test further down.
//
// GET /api/v1/profile/mfa/enrollment/qr carries no {id} placeholder but is
// excluded the same way: MfaEnrollmentQR (handlers/totp_api.go) 404s by
// design whenever the caller has no pending TOTP enrollment, so a bare
// "every sufficient role gets 200" assertion is false regardless of role —
// it needs POST /api/v1/profile/mfa/enrollment first. It gets its own
// dedicated setup+assert test below, same pattern as the key-download
// boundary case.
const MFA_QR_PATTERN = "/api/v1/profile/mfa/enrollment/qr";
const STATIC_GETS = REQUIRED_ROUTES.filter(
  (route) => route.method === "GET" && !route.pattern.includes("{") && route.pattern !== MFA_QR_PATTERN,
);
const MUTATIONS = REQUIRED_ROUTES.filter((route) => route.method !== "GET");

test.describe("E2E-RBAC-01: the route-by-role matrix, driven from backend/testdata/roles.golden", () => {
  test("E2E-RBAC-01: the golden table yields a non-empty required-route matrix", () => {
    expect(REQUIRED_ROUTES.length, "derived from backend/testdata/roles.golden, not main.go").toBeGreaterThan(0);
    expect(STATIC_GETS.length).toBeGreaterThan(0);
    expect(MUTATIONS.length).toBeGreaterThan(0);
  });

  for (const route of STATIC_GETS) {
    for (const role of ROLES) {
      const ok = sufficient(role, route);
      test(`E2E-RBAC-01: ${role} GET ${route.pattern} is ${ok ? "200" : "403 (insufficient role)"}`, async ({
        viewer,
        manager,
        admin,
      }) => {
        const ctx: AuthedContext = { viewer, manager, admin }[role];
        const res = await ctx.send(() => ctx.ctx.get(route.pattern));
        if (ok) {
          expect(res.status(), `${role} GET ${route.pattern}`).toBe(200);
          // Every 200 cell carries a content assertion: a status code alone
          // only proves the handler was reached, not that it answered.
          const body = await res.text();
          expect(body.length, `${role} GET ${route.pattern} returned a non-empty body`).toBeGreaterThan(0);
        } else {
          await expectInsufficientRole(role, route, res);
        }
      });
    }
  }

  for (const route of MUTATIONS) {
    for (const role of ROLES) {
      if (sufficient(role, route)) continue;
      test(`E2E-RBAC-01: ${role} ${route.method} ${route.pattern} is denied before the CSRF check`, async ({
        viewer,
        manager,
        admin,
      }) => {
        const ctx: AuthedContext = { viewer, manager, admin }[role];
        const url = withID(route.pattern, 999999);
        // Deliberately no X-CSRF-Token: the role gate runs before csrfMiddleware
        // (api/middleware.go), so an insufficient role must be refused without
        // ever needing a valid token.
        const res = await ctx.send(() => send(ctx.ctx, route, url));
        await expectInsufficientRole(role, route, res);
      });
    }
  }

  /**
   * The RBAC boundary this whole matrix exists to state explicitly
   * (plans/e2e-tests.md's E2E-RBAC-01 entry): a viewer can see that a
   * certificate exists (GET .../certificates/{id}, 200) but cannot obtain
   * its key (GET .../certificates/{id}/key, 403). Neither cell states the
   * boundary alone.
   */
  test("E2E-RBAC-01: viewer sees GET /certificates/{id} but is refused GET .../key", async ({ viewer, manager }) => {
    const name = disposableName("rbac-boundary");
    const token = await manager.csrf("/api/v1/certificates");
    const issued = await manager.send(() =>
      apiMutation(
        manager.ctx,
        "POST",
        "/api/v1/certificates",
        { name, domain: `${name}.e2e.invalid`, template: "server", keyType: "EC:P-256", duration: "720h" },
        token,
      ),
    );
    expect(issued.status(), "precondition issuance").toBe(201);
    const id = ((await issued.json()) as { id: number }).id;

    const details = await viewer.send(() => viewer.ctx.get(`/api/v1/certificates/${id}`));
    expect(details.status(), "viewer may see that a certificate exists").toBe(200);
    expect((await details.json()) as { name: string }).toMatchObject({ name });

    const key = await viewer.send(() => viewer.ctx.get(`/api/v1/certificates/${id}/key`));
    expect(key.status(), "viewer may not obtain its key").toBe(403);
  });

  /**
   * GET /api/v1/profile/mfa/enrollment/qr's role requirement (role=viewer,
   * so every fixture role is sufficient) only shows through once a pending
   * enrollment exists: MfaEnrollmentQR 404s with none (handlers/
   * totp_api.go), which is a setup precondition, not a role gate. Each
   * role starts its own enrollment first — profile/mfa state is per-user,
   * so the three fixture accounts don't collide.
   */
  for (const role of ROLES) {
    test(`E2E-RBAC-01: ${role} GET ${MFA_QR_PATTERN} is 200 once enrollment is pending`, async ({
      viewer,
      manager,
      admin,
    }) => {
      const ctx: AuthedContext = { viewer, manager, admin }[role];
      const started = await ctx.send(() => apiMutation(ctx.ctx, "POST", "/api/v1/profile/mfa/enrollment"));
      expect(started.status(), `${role} POST /api/v1/profile/mfa/enrollment precondition`).toBe(200);

      const res = await ctx.send(() => ctx.ctx.get(MFA_QR_PATTERN));
      expect(res.status(), `${role} GET ${MFA_QR_PATTERN}`).toBe(200);
      const body = await res.body();
      expect(body.length, `${role} GET ${MFA_QR_PATTERN} returned a non-empty image`).toBeGreaterThan(0);
    });
  }
});

test.describe("E2E-RBAC-01: public and optional-auth routes stay reachable with no session", () => {
  const openRoutes = goldenRoutes().filter((route) => route.auth !== "required");

  test("the golden table carries at least one public or optional-auth route", () => {
    expect(openRoutes.length).toBeGreaterThan(0);
  });

  for (const route of openRoutes) {
    test(`E2E-RBAC-01: anonymous ${route.method} ${route.pattern} (auth=${route.auth}) is never gated by session`, async () => {
      const anon = await newJar();
      try {
        const url = withID(route.pattern, 999999);
        const res = await send(anon, route, url);
        // sessionMiddleware is the only thing in the chain that answers 401
        // (api/middleware.go), and it exempts public and optional routes
        // outright. A 401 here means one of those two silently gained a
        // RequireLogin-equivalent gate.
        expect(res.status(), `${route.method} ${route.pattern}`).not.toBe(401);
      } finally {
        await anon.dispose();
      }
    });
  }
});

test("E2E-RBAC-02: unauthenticated access to every required API route answers 401, it does not redirect", async () => {
  const anon = await newJar();
  try {
    for (const route of REQUIRED_ROUTES) {
      const url = withID(route.pattern, 999999);
      const res = await send(anon, route, url);
      const where = `${route.method} ${route.pattern}`;
      // sessionMiddleware answers a missing/invalid session with 401 (5.3);
      // the server-rendered era's 302-to-/login is gone along with the pages
      // it redirected to.
      expect(res.status(), `${where} with no session`).toBe(401);
      expect(res.headers()["location"], `${where} carries no Location header`).toBeUndefined();
      const body = (await res.json()) as { title?: string };
      expect(body.title, `${where} problem title`).toBe("Unauthorized");
    }
  } finally {
    await anon.dispose();
  }
});
