import { APIRequestContext, APIResponse } from "@playwright/test";
import { csrfMutationRoutes, MutationRoute, withID } from "../../helpers/routes.js";
import { newJar } from "../../helpers/session.js";
import { AuthedContext, expect, test } from "../../fixtures/test.js";

function contextFor(
  route: MutationRoute,
  anon: APIRequestContext,
  contexts: Record<"viewer" | "manager" | "admin", AuthedContext>,
): APIRequestContext {
  if (route.auth === "public" || route.auth === "optional") return anon;
  return contexts[route.role === "-" ? "viewer" : route.role].ctx;
}

async function expectCSRFProblem(route: MutationRoute, kind: string, res: APIResponse): Promise<void> {
  expect(res.status(), `${route.method} ${route.pattern} with ${kind} token`).toBe(403);
  expect(res.headers()["content-type"] ?? "", `${route.method} ${route.pattern} problem content type`).toContain(
    "application/problem+json",
  );
  const body = (await res.json()) as { type?: string; title?: string };
  expect(body.type, `${route.method} ${route.pattern} problem type`).toMatch(/\/csrf$/);
  expect(body.title, `${route.method} ${route.pattern} problem title`).toBe("Forbidden");
}

test.describe("E2E-CSRF-01: every API mutation rejects a missing and a wrong token", () => {
  test("E2E-CSRF-01: the role golden table covers every API mutation with CSRF", () => {
    const routes = csrfMutationRoutes();
    expect(routes.length, "derived from backend/testdata/roles.golden, not main.go").toBeGreaterThan(0);
    expect(routes.map((route) => `${route.method} ${route.pattern}`)).toContain("POST /api/v1/session");
    expect(routes.map((route) => `${route.method} ${route.pattern}`)).toContain("DELETE /api/v1/session");
    expect(routes.map((route) => `${route.method} ${route.pattern}`)).toContain("POST /api/v1/users");
    expect(routes.map((route) => `${route.method} ${route.pattern}`)).toContain("PATCH /api/v1/profile/preferences");
  });

  test("E2E-CSRF-01: missing and wrong headers are rejected before every mutation", async ({ viewer, manager, admin }) => {
    const anon = await newJar();
    try {
      // GET /api/v1/session is the only anonymous CSRF mint. It also gives
      // public operations a session-bound token to reject when wrong.
      expect((await anon.get("/api/v1/session")).status()).toBe(200);
      const contexts = { viewer, manager, admin };
      for (const route of csrfMutationRoutes()) {
        const ctx = contextFor(route, anon, contexts);
        const url = withID(route.pattern, 999999);
        await expectCSRFProblem(route, "missing", await ctx.fetch(url, { method: route.method, data: {} }));
        await expectCSRFProblem(
          route,
          "wrong",
          await ctx.fetch(url, { method: route.method, data: {}, headers: { "X-CSRF-Token": "wrong-value" } }),
        );
      }
    } finally {
      await anon.dispose();
    }
  });
});
