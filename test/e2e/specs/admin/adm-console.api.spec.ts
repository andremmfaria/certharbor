import { APIResponse } from "@playwright/test";
import { composeOrThrow, psql, waitHealthy } from "../../helpers/compose.js";
import { apiMutation } from "../../helpers/session.js";
import { AuthedContext, expect, test } from "../../fixtures/test.js";

interface ConsoleResult {
  success: boolean;
  exitCode: number;
  output: string;
}

// POST /api/v1/admin/console/run replaces the removed form endpoint. Its JSON
// result is the same command oracle the old template rendered in a <pre>.
async function runCommand(admin: AuthedContext, commandId: string): Promise<ConsoleResult> {
  const res: APIResponse = await admin.send(() =>
    apiMutation(admin.ctx, "POST", "/api/v1/admin/console/run", { commandId }),
  );
  expect(res.status(), `run ${commandId}`).toBe(200);
  return (await res.json()) as ConsoleResult;
}

test.describe.configure({ mode: "serial" });

test("E2E-ADM-02: ca.health with the CA up", async ({ admin }) => {
  const result = await runCommand(admin, "ca.health");
  expect(result.success).toBe(true);
  expect(result.output).toBe("ok");
});

test("E2E-ADM-03: ca.health with the CA down", async ({ admin }) => {
  test.setTimeout(300_000);
  await composeOrThrow("stop", "step-ca");
  try {
    const result = await runCommand(admin, "ca.health");
    expect(result.success, "the command reports failure").toBe(false);
    expect(result.exitCode).toBe(1);
    expect(/connection refused|no such host|lookup .* server misbehaving|dial tcp|timeout|deadline exceeded|no route to host|EOF/i.test(result.output), `expected a dial-level error, got: ${result.output}`).toBe(true);
    expect(result.output, "not the construction-failure string").not.toContain("CA client unavailable");
  } finally {
    await composeOrThrow("start", "step-ca");
    await waitHealthy("step-ca");
  }
});

test("E2E-ADM-04: the OS diagnostic commands still run", async ({ admin }) => {
  test.setTimeout(180_000);
  const commands = ["system.date", "system.hostname", "system.identity", "system.disk", "system.processes", "app.files", "openssl.version", "postgres.ready"];
  const results = new Map<string, ConsoleResult>();
  for (const id of commands) {
    const result = await runCommand(admin, id);
    results.set(id, result);
    expect(result.success, `${id} succeeded`).toBe(true);
    expect(result.exitCode, `${id} exit code`).toBe(0);
  }
  expect(results.get("postgres.ready")!.output).toContain("accepting connections");
  expect(results.get("system.identity")!.output).toContain("10001");
});

test("E2E-ADM-05: an unknown command_id is rejected and audited", async ({ admin }) => {
  const maxBefore = await psql("SELECT coalesce(max(id), 0) FROM auth_log");
  const res = await admin.send(() => apiMutation(admin.ctx, "POST", "/api/v1/admin/console/run", { commandId: "rm.rf" }));
  expect(res.status()).toBe(422);

  const denied = await psql(`SELECT count(*) FROM auth_log WHERE id > ${maxBefore} AND reason LIKE '%console.denied command_id=rm.rf%'`);
  expect(Number(denied), "the denial is audited").toBeGreaterThan(0);
  const ran = await psql(`SELECT count(*) FROM auth_log WHERE id > ${maxBefore} AND reason LIKE '%console.run id=rm.rf%'`);
  expect(ran, "no console.run row for a command that never ran").toBe("0");
});
