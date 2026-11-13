import { createHash } from "node:crypto";
import { psql, run } from "../../helpers/compose.js";
import { apiMutation } from "../../helpers/session.js";
import { expect, test } from "../../fixtures/test.js";

interface BackupComponent {
  name: string;
  path: string;
  size: number;
  sha256: string;
  status: string;
  detail?: string;
}

interface Manifest {
  format: string;
  components: BackupComponent[];
  warnings?: string[];
}

interface Backup {
  id: string;
  sizeBytes: number;
  downloadUrl: string;
}

// Must not run against compose.e2e-fingerprint.yml, which removes /home/step
// and with it the step-ca-data component (Section 2.7.1).
test("E2E-BAK-01: the bundle is valid, complete and self-verifying", async ({ admin }) => {
  test.setTimeout(300_000);

  const fs = await import("node:fs/promises");
  const os = await import("node:os");
  const path = await import("node:path");
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "e2e-backup-"));

  try {
    // API contract is create-then-download, rather than the removed form POST
    // which streamed an archive immediately.
    const created = await admin.send(() => apiMutation(admin.ctx, "POST", "/api/v1/admin/backups"));
    expect(created.status()).toBe(200);
    const backup = (await created.json()) as Backup;
    expect(backup.id).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(backup.sizeBytes).toBeGreaterThan(0);
    expect(backup.downloadUrl).toBe(`/api/v1/admin/backups/${backup.id}/download`);

    const res = await admin.send(() => admin.ctx.get(backup.downloadUrl));
    expect(res.status()).toBe(200);
    expect(res.headers()["content-type"]).toBe("application/gzip");
    expect(res.headers()["content-disposition"]).toBe(`attachment; filename="${backup.id}.tgz"`);

    const bundle = path.join(dir, "backup.tgz");
    await fs.writeFile(bundle, await res.body());

    const listed = await run(["tar", "tzf", bundle], { timeoutMs: 60_000 });
    expect(listed.code, `tar tzf: ${listed.stderr}`).toBe(0);
    const entries = listed.stdout.split("\n").map((l) => l.trim()).filter(Boolean);
    for (const expected of ["manifest.json", "postgres-certharbor.sql", "step-ca-data.tgz", "certharbor-data.tgz", "certharbor-certs.tgz", "certharbor-uploads.tgz"]) {
      expect(entries, `${expected} is in the bundle`).toContain(expected);
    }

    const extracted = path.join(dir, "x");
    await fs.mkdir(extracted);
    const untar = await run(["tar", "xzf", bundle, "-C", extracted], { timeoutMs: 120_000 });
    expect(untar.code, `tar xzf: ${untar.stderr}`).toBe(0);

    const manifest = JSON.parse(await fs.readFile(path.join(extracted, "manifest.json"), "utf8")) as Manifest;
    expect(manifest.format).toBe("certharbor-backup-v1");
    expect(manifest.components.map((c) => c.name).sort()).toEqual(["postgres", "certharbor-certs", "certharbor-data", "certharbor-uploads"].sort());
    expect(manifest.warnings ?? []).toEqual(["step-ca-data failed: open /home/step/config: permission denied"]);

    for (const component of manifest.components) {
      expect(component.status, `${component.name} status`).toBe("ok");
      const bytes = await fs.readFile(path.join(extracted, path.basename(component.path)));
      const digest = createHash("sha256").update(bytes).digest("hex");
      expect(digest, `${component.name} (${component.path}) digest matches the manifest`).toBe(component.sha256);
    }

    const partial = await run(["tar", "tzf", path.join(extracted, "step-ca-data.tgz")], { timeoutMs: 60_000 });
    expect(partial.code, `step-ca-data.tgz is a readable tarball: ${partial.stderr}`).toBe(0);
    const inner = partial.stdout.split("\n").map((l) => l.trim()).filter(Boolean);
    expect(inner.filter((e) => /^\.?\/?certs\/.+/.test(e)).length, "certs/ made it in with its contents").toBeGreaterThan(0);
    expect(inner.filter((e) => /^\.?\/?config\/.+/.test(e)), "config/ carries no contents").toEqual([]);
    expect(inner.filter((e) => /^\.?\/?secrets\//.test(e)), "secrets/ was never reached").toEqual([]);

    const dump = await fs.readFile(path.join(extracted, "postgres-certharbor.sql"), "utf8");
    for (const table of ["certificates", "users", "cert_history"]) {
      expect(new RegExp(`(COPY|INSERT INTO) (public\\.)?${table}\\b`).test(dump), `the dump carries ${table} as plain SQL`).toBe(true);
    }

    const audited = await psql(`SELECT count(*) FROM auth_log WHERE reason = 'backup.create id=${backup.id}'`);
    expect(Number(audited), "the create is audited").toBeGreaterThan(0);
  } finally {
    await fs.rm(dir, { recursive: true, force: true });
  }
});
