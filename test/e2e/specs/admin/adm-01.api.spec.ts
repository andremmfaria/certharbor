import { apiMutation } from "../../helpers/session.js";
import { expect, test } from "../../fixtures/test.js";

test("E2E-ADM-01: app.version pins the certificates library", async ({ admin }) => {
  const commands = await admin.send(() => admin.ctx.get("/api/v1/admin/console/commands"));
  expect(commands.status()).toBe(200);
  const listed = (await commands.json()) as Array<{ id: string }>;
  expect(listed.map((command) => command.id)).toEqual(expect.arrayContaining(["app.version", "ca.health"]));

  const res = await admin.send(() =>
    apiMutation(admin.ctx, "POST", "/api/v1/admin/console/run", { commandId: "app.version" }),
  );
  expect(res.status()).toBe(200);
  const result = (await res.json()) as { success: boolean; exitCode: number; output: string };
  expect(result.success).toBe(true);
  expect(result.exitCode).toBe(0);
  expect(result.output.split("\n")[1]).toBe("smallstep/certificates v0.30.2");

  const security = await admin.send(() => admin.ctx.get("/api/v1/security/events?pageSize=200"));
  expect(security.status()).toBe(200);
  const events = (await security.json()) as { items: Array<{ reason?: string }> };
  // The audit row carries the "Audit: " prefix (handlers/audit.go's
  // auditPrefix) plus the command/exit/timeout/duration fields
  // RunAdminConsoleCommand appends (handlers/admin_console.go), so only the
  // leading "id=app.version" segment is a stable, non-timing-dependent match.
  expect(events.items.some((event) => event.reason?.startsWith("Audit: console.run id=app.version"))).toBe(true);
});
