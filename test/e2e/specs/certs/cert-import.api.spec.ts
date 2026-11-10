import {
  createCertificate,
  getCertificateCertificate,
  getCertificateKey,
  importCertificateUpload,
  importCertificatesByPaths,
  listCertificateImportCandidates,
} from "@certharbor/client";
import type { BulkResult, ImportCandidateBody } from "@certharbor/client";
import { psql } from "../../helpers/compose.js";
import { ApiSession, authedClient, blobText } from "../../helpers/apiClient.js";
import { FIXTURE_USERS } from "../../fixtures/test.js";
import { expect, test } from "@playwright/test";

// Covers the import family against the generated client (clients/ts),
// replacing the legacy multipart-form POST /import (Section 6: "two
// operations by payload"): POST /api/v1/certificates/import/upload (upload),
// GET /api/v1/certificates/import-candidates + POST
// /api/v1/certificates/import/paths (scan/manual, both served by the one
// paths operation), and the per-item BulkResponse behaviour that replaces
// the old import/scan's swallowed count.

const CERTS_DIR = "/opt/certharbor/certs";

let manager: ApiSession;

test.beforeAll(async () => {
  manager = await authedClient(FIXTURE_USERS.manager);
});

/** Issues a disposable certificate through the API and returns its PEM cert+key, as out-of-band material for an import test. */
async function issueMaterial(namePrefix: string): Promise<{ id: number; name: string; certPem: string; keyPem: string }> {
  const name = `${namePrefix}-${Date.now().toString(36)}-${Math.floor(Math.random() * 1e6).toString(36)}`;
  const issued = await manager.call(() =>
    createCertificate({ body: { name, domain: `${name}.example.com`, template: "server", keyType: "EC:P-256", duration: "8760h" } }),
  );
  expect(issued.response?.status, `precondition issuance of ${name}`).toBe(201);
  const id = issued.data!.id;
  const certRes = await manager.call(() => getCertificateCertificate({ path: { id } }));
  const keyRes = await manager.call(() => getCertificateKey({ path: { id } }));
  const certPem = await blobText(certRes.data);
  const keyPem = await blobText(keyRes.data);
  return { id, name, certPem, keyPem };
}

test("E2E-CERT-06: import by upload succeeds via multipart through the generated client", async () => {
  const material = await issueMaterial("e2e-import-upload-src");
  const importName = `e2e-import-upload-${Date.now().toString(36)}`;
  const domain = `${importName}.example.com`;

  const res = await manager.call(() =>
    importCertificateUpload({
      body: {
        name: importName,
        domain,
        certFile: new File([material.certPem], "certificate.crt", { type: "application/octet-stream" }),
        keyFile: new File([material.keyPem], "private.key", { type: "application/octet-stream" }),
      },
    }),
  );
  expect(res.error, "POST /api/v1/certificates/import/upload").toBeUndefined();
  expect(res.response?.status).toBe(201);
  expect(res.data?.name).toBe(importName);
  expect(res.data?.domain).toBe(domain);
  expect(res.data?.keyType).toBe("EC:P-256");

  const row = await psql(`SELECT domain FROM certificates WHERE name = '${importName}'`);
  expect(row.trim()).toBe(domain);
  const history = await psql(`SELECT count(*) FROM cert_history WHERE action = 'import' AND cert_name = '${importName}'`);
  expect(Number(history), "an import history row exists").toBeGreaterThan(0);
});

test("E2E-CERT-07: import-candidates finds an unregistered file, and import/paths imports it manually", async () => {
  // A certificate whose row is deleted leaves its file behind untouched
  // (ImportCertificateUpload/Issue never delete on their own), which is
  // exactly what scanExistingCerts's already-registered check is for: no
  // cert_history row references certificates.id (schema.go), so the delete
  // is clean and the file becomes a genuine unregistered candidate.
  const material = await issueMaterial("e2e-import-candidate");
  await psql(`DELETE FROM certificates WHERE id = ${material.id}`);

  const candidates = await manager.call(() => listCertificateImportCandidates({}));
  expect(candidates.error, "GET /api/v1/certificates/import-candidates").toBeUndefined();
  const found = (candidates.data ?? []).find((c: ImportCandidateBody) => c.name === material.name);
  expect(found, `${material.name} appears as an import candidate once its row is gone`).toBeTruthy();
  expect(found!.certFile, "candidate certFile is absolute (CertsDir-rooted)").toContain(CERTS_DIR);

  // import/paths' certPath/keyPath are resolved against CertsDir and must be
  // relative to it (certificates_integration_test.go: "good-item/certificate.crt"),
  // not the absolute path the candidate listing reports.
  const relCert = found!.certFile.replace(`${CERTS_DIR}/`, "");
  const relKey = found!.keyFile ? found!.keyFile.replace(`${CERTS_DIR}/`, "") : undefined;

  const importedDomain = `${material.name}-reimported.example.com`;
  const res = await manager.call(() =>
    importCertificatesByPaths({
      body: [{ name: material.name, domain: importedDomain, certPath: relCert, keyPath: relKey }],
    }),
  );
  expect(res.error, "POST /api/v1/certificates/import/paths").toBeUndefined();
  expect(res.response?.status).toBe(200);
  const results = res.data?.results ?? [];
  expect(results).toHaveLength(1);
  expect(results[0]?.status, `import of ${material.name}: ${results[0]?.error ?? ""}`).toBe("ok");
  expect(results[0]?.ref).toBe(material.name);

  const row = await psql(`SELECT domain FROM certificates WHERE name = '${material.name}'`);
  expect(row.trim()).toBe(importedDomain);
});

test("E2E-CERT-08: import/paths rejects a path that escapes the managed certificates directory", async () => {
  const name = `e2e-traversal-${Date.now().toString(36)}`;
  const before = (await psql(`SELECT count(*) FROM certificates WHERE name = '${name}'`)).trim();
  expect(before).toBe("0");

  const res = await manager.call(() =>
    importCertificatesByPaths({
      body: [{ name, domain: "traversal.example.com", certPath: "../../../../etc/passwd" }],
    }),
  );
  // Every item gets a result, never a problem document (5.2's bulk-result
  // convention): a traversal attempt is still a 200 with a per-item error,
  // the same shape a missing file gets.
  expect(res.error, "POST /api/v1/certificates/import/paths").toBeUndefined();
  expect(res.response?.status).toBe(200);
  const results = res.data?.results ?? [];
  expect(results).toHaveLength(1);
  expect(results[0]?.status).toBe("error");
  expect(results[0]?.error ?? "").toContain("escapes root");

  const after = (await psql(`SELECT count(*) FROM certificates WHERE name = '${name}'`)).trim();
  expect(after, "no row was created by the rejected item").toBe("0");
});

test("E2E-CERT-14: import/paths reports a mixed success/failure batch per item, not a swallowed count", async () => {
  const material = await issueMaterial("e2e-import-mixed-good");
  await psql(`DELETE FROM certificates WHERE id = ${material.id}`);
  const relCert = (
    (await manager.call(() => listCertificateImportCandidates({}))).data ?? []
  ).find((c: ImportCandidateBody) => c.name === material.name)!.certFile.replace(`${CERTS_DIR}/`, "");

  const badName = `e2e-import-mixed-bad-${Date.now().toString(36)}`;

  const res = await manager.call(() =>
    importCertificatesByPaths({
      body: [
        { name: material.name, domain: `${material.name}-mixed.example.com`, certPath: relCert },
        { name: badName, domain: "mixed-bad.example.com", certPath: `${badName}/certificate.crt` },
      ],
    }),
  );
  expect(res.error, "POST /api/v1/certificates/import/paths").toBeUndefined();
  expect(res.response?.status, "always 200, even with a failing item in the batch").toBe(200);

  const results = res.data?.results ?? [];
  expect(results, "one result per submitted item").toHaveLength(2);

  const good = results.find((r: BulkResult) => r.ref === material.name);
  const bad = results.find((r: BulkResult) => r.ref === badName);
  expect(good, `${material.name}'s own result is present`).toBeTruthy();
  expect(bad, `${badName}'s own result is present`).toBeTruthy();
  expect(good!.status, "the valid item succeeds").toBe("ok");
  expect(bad!.status, "the invalid item fails").toBe("error");
  expect(bad!.error ?? "", "the invalid item's error names the missing file").toContain("not found");

  // The defining behaviour change from the old import/scan: one item's
  // failure never discards the other item's success, and the success is
  // never reported as merely "found and imported: N".
  const goodRow = await psql(`SELECT count(*) FROM certificates WHERE name = '${material.name}'`);
  expect(Number(goodRow), "the valid item's row was actually written").toBe(1);
  const badRow = await psql(`SELECT count(*) FROM certificates WHERE name = '${badName}'`);
  expect(Number(badRow), "the invalid item wrote no row").toBe(0);
});
