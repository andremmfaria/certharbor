import {
  createCertificate,
  getCertificateCertificate,
  getCertificateKey,
  getCertificateOptions,
  listCertificates,
} from "@certharbor/client";
import { logs } from "../../helpers/compose.js";
import { ApiSession, authedClient, blobText } from "../../helpers/apiClient.js";
import { assertKeyPairs, dnsNames, expectedKey, keyDetails } from "../../helpers/certs.js";
import { parsePEM, validityMs } from "../../helpers/openssl.js";
import { FIXTURE_USERS } from "../../fixtures/test.js";
import { expect, test } from "@playwright/test";

// Rewritten against POST /api/v1/certificates, GET /api/v1/certificates/options,
// GET /api/v1/certificates?q=, GET /api/v1/certificates/{id}/certificate and
// GET /api/v1/certificates/{id}/key, all through the generated client
// (clients/ts). Section 6: "the field is name, not cert_name".

const HOUR = 3_600_000;
const MINUTE = 60_000;

interface IssueRequest {
  name: string;
  domain: string;
  template: "server" | "internal" | "wildcard" | "client";
  keyType: "EC:P-256" | "EC:P-384" | "RSA:2048" | "RSA:4096";
  duration: "720h" | "4380h" | "8760h" | "87600h";
}

const MATRIX: IssueRequest[] = [
  // The boundary probe runs first: 87600h equals STEPCA_MAX_TLS_CERT_DURATION
  // exactly, and normalizeIssuePolicy rejects only what exceeds the maximum.
  { name: "e2e-internal-ec-p256", template: "internal", domain: "e2e-internal.example.com", duration: "87600h", keyType: "EC:P-256" },

  { name: "e2e-server-ec-p256", template: "server", domain: "e2e-server.example.com", duration: "8760h", keyType: "EC:P-256" },
  { name: "e2e-server-ec-p384", template: "server", domain: "e2e-server.example.com", duration: "8760h", keyType: "EC:P-384" },
  { name: "e2e-server-rsa2048", template: "server", domain: "e2e-server.example.com", duration: "8760h", keyType: "RSA:2048" },
  { name: "e2e-server-rsa4096", template: "server", domain: "e2e-server.example.com", duration: "8760h", keyType: "RSA:4096" },

  { name: "e2e-wildcard-ec-p256", template: "wildcard", domain: "*.e2e-wildcard.example.com", duration: "8760h", keyType: "EC:P-256" },
  { name: "e2e-client-ec-p256", template: "client", domain: "e2e-client.example.com", duration: "8760h", keyType: "EC:P-256" },

  { name: "e2e-dur-720h", template: "server", domain: "e2e-dur.example.com", duration: "720h", keyType: "EC:P-256" },
  { name: "e2e-dur-4380h", template: "server", domain: "e2e-dur.example.com", duration: "4380h", keyType: "EC:P-256" },
  { name: "e2e-dur-8760h", template: "server", domain: "e2e-dur.example.com", duration: "8760h", keyType: "EC:P-256" },
  { name: "e2e-dur-87600h", template: "server", domain: "e2e-dur.example.com", duration: "87600h", keyType: "EC:P-256" },
];

const observedKeys = new Map<string, string>();
const observedValidity = new Map<string, number>();

let manager: ApiSession;

test.beforeAll(async () => {
  manager = await authedClient(FIXTURE_USERS.manager);
});

test.describe.serial("E2E-CERT-01: issuance matrix", () => {
  test("E2E-CERT-01: GET /api/v1/certificates/options lists every matrix template", async () => {
    const res = await manager.call(() => getCertificateOptions({}));
    expect(res.error, "options request succeeded").toBeUndefined();
    const ids = (res.data?.templates ?? []).map((t: { id: string }) => t.id);
    for (const row of MATRIX) {
      expect(ids, `options catalogue includes the ${row.template} template`).toContain(row.template);
    }
  });

  for (const row of MATRIX) {
    test(`E2E-CERT-01: ${row.name} (${row.template}, ${row.keyType}, ${row.duration})`, async () => {
      test.setTimeout(180_000);

      const created = await manager.call(() =>
        createCertificate({
          body: { name: row.name, domain: row.domain, template: row.template, duration: row.duration, keyType: row.keyType },
        }),
      );
      expect(created.error, `POST /api/v1/certificates for ${row.name}`).toBeUndefined();
      expect(created.response?.status).toBe(201);
      const summary = created.data!;
      expect(summary.name).toBe(row.name);
      expect(summary.domain).toBe(row.domain);
      expect(summary.keyType).toBe(row.keyType);

      // GET /api/v1/certificates?q= exercises the list operation the matrix
      // would otherwise never touch, and confirms the new row is findable by
      // the same query the certificates page issues.
      const listed = await manager.call(() => listCertificates({ query: { q: row.name } }));
      expect(listed.error, `GET /api/v1/certificates?q=${row.name}`).toBeUndefined();
      const match = (listed.data?.items ?? []).find((c: { id: number }) => c.id === summary.id);
      expect(match, `${row.name} appears in its own list query`).toBeTruthy();

      const certRes = await manager.call(() => getCertificateCertificate({ path: { id: summary.id } }));
      expect(certRes.error, `GET /api/v1/certificates/${summary.id}/certificate`).toBeUndefined();
      const keyRes = await manager.call(() => getCertificateKey({ path: { id: summary.id } }));
      expect(keyRes.error, `GET /api/v1/certificates/${summary.id}/key`).toBeUndefined();

      const certPem = await blobText(certRes.data);
      const keyPem = await blobText(keyRes.data);
      const cert = await parsePEM(certPem);

      expect(cert.subject, "Subject.CommonName is the requested domain").toContain(`CN=${row.domain}`);
      // Risk R4's CSR-shape half: both a CN-only and a DNSNames-only CSR would
      // satisfy "contains the domain", the two wrong shapes.
      expect(dnsNames(cert), `${row.name} DNSNames`).toEqual([row.domain]);

      const details = await keyDetails(cert.pem);
      expect(details, `${row.name} key parameters read from the downloaded certificate`).toEqual(expectedKey(row.keyType));
      await assertKeyPairs(cert.pem, keyPem);

      const requestedMs = Number(row.duration.replace("h", "")) * HOUR;
      expect(Math.abs(validityMs(cert) - requestedMs), `${row.name} validity within a minute of ${row.duration}`).toBeLessThanOrEqual(MINUTE);

      observedKeys.set(row.name, `${details.algorithm}/${details.curve}/${details.rsaBits}`);
      observedValidity.set(row.name, validityMs(cert));
    });
  }

  test("E2E-CERT-01: the key axis yields four distinct public-key parameter sets", () => {
    const axis = ["e2e-server-ec-p256", "e2e-server-ec-p384", "e2e-server-rsa2048", "e2e-server-rsa4096"];
    const seen = axis.map((n) => observedKeys.get(n));
    expect(seen, "every key-axis row was observed").not.toContain(undefined);
    // A build stuck on EC P-256 fails here and nowhere else.
    expect(new Set(seen).size, `distinct key parameters across ${axis.join(", ")}: ${seen.join(" | ")}`).toBe(4);
  });

  test("E2E-CERT-01: the duration axis yields four distinct windows in the requested order", () => {
    const axis = ["e2e-dur-720h", "e2e-dur-4380h", "e2e-dur-8760h", "e2e-dur-87600h"];
    const seen = axis.map((n) => observedValidity.get(n));
    expect(seen, "every duration-axis row was observed").not.toContain(undefined);
    expect(new Set(seen).size, `distinct validity windows: ${seen.join(" | ")}`).toBe(4);
    const values = seen as number[];
    for (let i = 1; i < values.length; i++) {
      expect(values[i]!, `${axis[i]} is longer than ${axis[i - 1]}`).toBeGreaterThan(values[i - 1]!);
    }
  });
});

test("E2E-CERT-02: the wildcard template rejects a non-wildcard domain", async () => {
  const before = (await manager.call(() => listCertificates({ query: { pageSize: 1 } }))).data?.total ?? 0;

  const res = await manager.call(() =>
    createCertificate({
      body: { name: "e2e-wildcard-rejected", domain: "not-a-wildcard.example.com", template: "wildcard", keyType: "EC:P-256", duration: "8760h" },
    }),
  );

  // 5.2's rule: caller-input errors answer 422, carried through mapCertError
  // from handlers.ValidationError (checkDomainPolicy's wildcard branch).
  expect(res.response?.status).toBe(422);
  expect(JSON.stringify(res.error)).toContain("wildcard template requires domain like *.example.com");

  const after = (await manager.call(() => listCertificates({ query: { pageSize: 1 } }))).data?.total ?? 0;
  expect(after, "no row was created").toBe(before);
});

test("E2E-CERT-03: an invalid domain is rejected before it reaches the CA", async () => {
  test.setTimeout(180_000);

  // Positive control first: an absence assertion over a log is unfalsifiable
  // on its own, so the pattern must be shown to fire in this environment
  // before anything is concluded from its absence.
  const controlName = `e2e-control-${Date.now().toString(36)}`;
  const controlSince = new Date().toISOString();
  const control = await manager.call(() =>
    createCertificate({ body: { name: controlName, domain: "e2e-control.example.com", template: "server", keyType: "EC:P-256", duration: "8760h" } }),
  );
  expect(control.response?.status, "control issuance").toBe(201);

  const SIGN_PATTERN = "/sign";
  const controlLog = await logs("step-ca", { since: controlSince });
  const controlHits = controlLog.split("\n").filter((l) => l.includes(SIGN_PATTERN)).length;
  expect(controlHits, `positive control: the CA logs ${SIGN_PATTERN} in this environment`).toBeGreaterThan(0);

  const since = new Date().toISOString();

  const flagInjection = await manager.call(() =>
    createCertificate({ body: { name: "e2e-badid-dash", domain: "--foo", template: "server", keyType: "EC:P-256", duration: "8760h" } }),
  );
  expect(flagInjection.response?.status).toBe(422);
  expect(JSON.stringify(flagInjection.error)).toContain("starts with");
  expect(JSON.stringify(flagInjection.error)).toContain("possible flag injection");

  const shellish = await manager.call(() =>
    createCertificate({ body: { name: "e2e-badid-shell", domain: "; rm -rf /", template: "server", keyType: "EC:P-256", duration: "8760h" } }),
  );
  expect(shellish.response?.status).toBe(422);
  expect(JSON.stringify(shellish.error)).toContain("contains disallowed characters");

  const after = await logs("step-ca", { since });
  const hits = after.split("\n").filter((l) => l.includes(SIGN_PATTERN));
  expect(hits.length, `no signing request reached the CA; matched lines:\n${hits.join("\n")}`).toBe(0);
});
