import {
  createCertificate,
  getCertificate,
  getCertificateCertificate,
  renewCertificate,
  revokeCertificate,
} from "@kantharos-ai/certharbor-client";
import { psql } from "../../helpers/compose.js";
import { ApiSession, authedClient, blobText } from "../../helpers/apiClient.js";
import { keyDetails } from "../../helpers/certs.js";
import { parsePEM, validityMs } from "../../helpers/openssl.js";
import { adminCredentials, FIXTURE_USERS } from "../../fixtures/test.js";
import { expect, test } from "@playwright/test";

// Rewritten against POST /api/v1/certificates/{id}/renew and
// POST /api/v1/certificates/{id}/revoke, both through the generated client.
// E2E-CERT-04 and E2E-CERT-05 must operate on different certificates: renewal
// and revocation interfere (Section 3.0.3). Each issues its own subject
// rather than depending on E2E-CERT-01's residue, so this file is runnable
// on its own.

let manager: ApiSession;
let admin: ApiSession;

test.beforeAll(async () => {
  manager = await authedClient(FIXTURE_USERS.manager);
  admin = await authedClient(adminCredentials());
});

test("E2E-CERT-04: renew", async () => {
  test.setTimeout(180_000);

  const subject = `e2e-renew-${Date.now().toString(36)}`;
  const issued = await manager.call(() =>
    createCertificate({ body: { name: subject, domain: `${subject}.example.com`, template: "server", keyType: "EC:P-256", duration: "8760h" } }),
  );
  expect(issued.response?.status, "precondition issuance").toBe(201);
  const originalID = issued.data!.id;

  const originalCertRes = await manager.call(() => getCertificateCertificate({ path: { id: originalID } }));
  const original = await parsePEM(await blobText(originalCertRes.data));
  const originalKey = await keyDetails(original.pem);
  const originalValidity = validityMs(original);
  const rowsBefore = Number(await psql(`SELECT count(*) FROM certificates WHERE name = '${subject}'`));

  // X.509 NotBefore/NotAfter have one-second resolution, and the harness
  // issues + renews fast enough to land in the same wall-clock second,
  // which would make the "later expiry" assertion below flaky rather than
  // a real signal of renewal working. Crossing a second boundary first
  // guarantees the renewed cert's timestamp actually differs.
  await new Promise((resolve) => setTimeout(resolve, 1100));

  const res = await manager.call(() => renewCertificate({ path: { id: originalID } }));
  expect(res.error, `POST /api/v1/certificates/${originalID}/renew`).toBeUndefined();
  expect(res.response?.status).toBe(200);
  const renewedSummary = res.data!;
  expect(renewedSummary.name).toBe(subject);

  // Renew inserts a new row rather than updating the old one, so the stale
  // pre-renewal serial survives in the database while the file holds the new
  // certificate. The file is the oracle; neither row is.
  const rowsAfter = Number(await psql(`SELECT count(*) FROM certificates WHERE name = '${subject}'`));
  expect(rowsAfter, "renew inserts, it does not update").toBe(rowsBefore + 1);
  expect(renewedSummary.id).not.toBe(originalID);

  const renewedCertRes = await manager.call(() => getCertificateCertificate({ path: { id: renewedSummary.id } }));
  const renewed = await parsePEM(await blobText(renewedCertRes.data));
  expect(renewed.serial, "the file carries a new serial").not.toBe(original.serial);
  expect(renewed.notAfter.getTime(), "and a later expiry").toBeGreaterThan(original.notAfter.getTime());

  // Renew reuses the stored KeyType/IssueDuration, falling back only for rows
  // that predate those columns.
  expect(await keyDetails(renewed.pem), "key type is carried over").toEqual(originalKey);
  expect(Math.abs(validityMs(renewed) - originalValidity), "validity window is carried over").toBeLessThanOrEqual(60_000);

  const history = await psql(`SELECT count(*) FROM cert_history WHERE action = 'renew' AND cert_name = '${subject}'`);
  expect(Number(history), "a renew history row exists").toBeGreaterThan(0);

  // GET /api/v1/certificates/{id} (the detail operation) sees the renewed row.
  const detail = await manager.call(() => getCertificate({ path: { id: renewedSummary.id } }));
  expect(detail.error, `GET /api/v1/certificates/${renewedSummary.id}`).toBeUndefined();
  expect(detail.data?.subject).toContain(`CN=${subject}.example.com`);
});

test("E2E-CERT-05: revoke changes status and is admin-only", async () => {
  test.setTimeout(120_000);

  const subject = `e2e-revoke-${Date.now().toString(36)}`;
  const issued = await manager.call(() =>
    createCertificate({ body: { name: subject, domain: `${subject}.example.com`, template: "server", keyType: "EC:P-256", duration: "8760h" } }),
  );
  expect(issued.response?.status, "precondition issuance").toBe(201);
  const id = issued.data!.id;

  // Section 6 flags this role split as invisible from the page layout: every
  // other certificate mutation in the table is manager, revoke is admin.
  const forbidden = await manager.call(() => revokeCertificate({ path: { id } }));
  expect(forbidden.response?.status, "manager may not revoke").toBe(403);

  const res = await admin.call(() => revokeCertificate({ path: { id } }));
  expect(res.error, `POST /api/v1/certificates/${id}/revoke`).toBeUndefined();
  expect(res.response?.status).toBe(200);
  expect(res.data?.status).toBe("revoked");

  const row = await psql(`SELECT status FROM certificates WHERE id = ${id}`);
  expect(row.trim()).toBe("revoked");

  const history = await psql(`SELECT count(*) FROM cert_history WHERE action = 'revoke' AND cert_name = '${subject}'`);
  expect(Number(history), "a revoke history row exists").toBeGreaterThan(0);
});
