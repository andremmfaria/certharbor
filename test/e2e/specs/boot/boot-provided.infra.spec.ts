import * as fs from "node:fs/promises";
import * as os from "node:os";
import * as path from "node:path";
import { expect, test } from "@playwright/test";
import { backendAddress, composeArgv, composeOrThrow, logs, projectName, run, upFlags, upStack, waitHealthy } from "../../helpers/compose";
import { setEnvKeys } from "../../helpers/envfile";
import { parsePEM, probeTLS } from "../../helpers/openssl";

test("E2E-BOOT-03: UI_TLS_MODE=provided leaves an operator certificate untouched", async () => {
  test.setTimeout(4 * 60_000);

  await composeOrThrow("down", "-v");
  await composeOrThrow("up", "-d", "--wait", "postgres", "step-ca");
  // Creating, not starting, certharbor-control makes compose create its volumes, so
  // the `down -v` of every later scenario removes the seeded one again.
  const created = await run(composeArgv("create", ...upFlags(), "certharbor-control"), { timeoutMs: 5 * 60_000 });
  expect(created.code, `docker compose create certharbor-control failed: ${created.stderr}`).toBe(0);

  // Generated on the harness, not inside the runtime image (uid 10001 cannot
  // apk add), then copied into the named volume by a throwaway container so
  // it is in place before certharbor-control's first start, "provided" never writes
  // anything, so nothing else would put it there.
  const tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), "e2e-boot03-"));
  try {
    const gen = await run([
      "openssl", "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-days", "1", "-nodes",
      "-subj", "/CN=test-provided", "-keyout", path.join(tmpDir, "server.key"), "-out", path.join(tmpDir, "server.crt"),
    ]);
    expect(gen.code, `openssl req failed: ${gen.stderr}`).toBe(0);
    const seededPEM = await fs.readFile(path.join(tmpDir, "server.crt"), "utf8");
    const seededCert = await parsePEM(seededPEM);

    const vol = `${projectName()}_certharbor-control-ssl`;
    const seed = await run([
      "docker", "run", "--rm",
      "-v", `${vol}:/ssl`,
      "-v", `${tmpDir}:/src:ro`,
      "alpine",
      // The directory too: this container creates the volume, so it is
      // root-owned, and the backend writes each leaf as a .tmp sibling it
      // renames into place, which needs the directory writable.
      "sh", "-c", "cp /src/server.crt /src/server.key /ssl/ && chown 10001:10001 /ssl /ssl/server.crt /ssl/server.key",
    ]);
    expect(seed.code, `seeding the operator certificate failed: ${seed.stderr}`).toBe(0);

    setEnvKeys({ UI_TLS_MODE: "provided" });
    await upStack();
    await waitHealthy("certharbor-control", 180_000);

    const served = await probeTLS(await backendAddress());
    expect(served.subject).toBe(seededCert.subject);
    expect(served.sha256).toBe(seededCert.sha256);

    // Negative control: the same pre-seeded material stays in place, but a
    // changed UI_TLS_MODE with a reachable CA now issues a different leaf.
    // Without this, "no bootstrap log lines in the provided case" would pass
    // just as well if the whole bootstrap block had been deleted.
    setEnvKeys({ UI_TLS_MODE: "stepca" });
    await upStack();
    await waitHealthy("certharbor-control", 180_000);

    const reissued = await probeTLS(await backendAddress());
    expect(reissued.subject).not.toBe(served.subject);
    expect(reissued.sha256).not.toBe(served.sha256);
    expect(await logs("certharbor-control")).toContain("UI leaf certificate obtained");
  } finally {
    await fs.rm(tmpDir, { recursive: true, force: true });
  }
});

test.afterAll(async () => {
  await composeOrThrow("down", "-v");
});
