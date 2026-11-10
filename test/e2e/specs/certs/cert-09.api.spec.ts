import {
  createCertificate,
  getCa,
  getCaChain,
  getCaIntermediate,
  getCaRoot,
  getCertificateCertificate,
  getCertificateKey,
} from "@certharbor/client";
import { run } from "../../helpers/compose.js";
import { ApiSession, authedClient, blobText } from "../../helpers/apiClient.js";
import { adminCredentials, FIXTURE_USERS } from "../../fixtures/test.js";
import { expect, test } from "@playwright/test";

// Rewritten against the generated client (clients/ts), replacing the legacy
// /download/* routes. Section 6's role split: the certificate's own
// cert/key files are manager, GET /api/v1/ca itself is viewer, and all three
// CA chain downloads (root/intermediate/chain) are admin-only.

let manager: ApiSession;
let admin: ApiSession;
let viewer: ApiSession;
let certID: number;
let certName: string;

test.beforeAll(async () => {
  manager = await authedClient(FIXTURE_USERS.manager);
  admin = await authedClient(adminCredentials());
  viewer = await authedClient(FIXTURE_USERS.viewer);

  certName = `e2e-cert09-${Date.now().toString(36)}`;
  const issued = await manager.call(() =>
    createCertificate({
      body: { name: certName, domain: `${certName}.example.com`, template: "server", keyType: "EC:P-256", duration: "8760h" },
    }),
  );
  expect(issued.response?.status, "precondition issuance").toBe(201);
  certID = issued.data!.id;
});

test("E2E-CERT-09: a certificate's own cert/key downloads carry the attachment filename and content", async () => {
  const cert = await manager.call(() => getCertificateCertificate({ path: { id: certID } }));
  expect(cert.response?.status, "GET .../certificate").toBe(200);
  expect(cert.response?.headers.get("content-type")).toBe("application/x-pem-file");
  expect(cert.response?.headers.get("content-disposition")).toBe(`attachment; filename=${certName}.crt`);
  expect(await blobText(cert.data)).toContain("BEGIN CERTIFICATE");

  const key = await manager.call(() => getCertificateKey({ path: { id: certID } }));
  expect(key.response?.status, "GET .../key").toBe(200);
  expect(key.response?.headers.get("content-type")).toBe("application/x-pem-file");
  expect(key.response?.headers.get("content-disposition")).toBe(`attachment; filename=${certName}.key`);
  expect(await blobText(key.data)).toContain("PRIVATE KEY");
});

test("E2E-CERT-09: GET /api/v1/ca is reachable at the viewer floor", async () => {
  const res = await viewer.call(() => getCa({}));
  expect(res.error, "GET /api/v1/ca as viewer").toBeUndefined();
  expect(res.response?.status).toBe(200);
  expect(res.data?.subject, "subject is populated").toBeTruthy();
  expect(res.data?.fingerprintSha256, "fingerprint is populated").toBeTruthy();
  expect(res.data?.reachable, "the CA is reachable in this environment").toBe(true);
});

test("E2E-CERT-09: the three CA chain downloads are admin-only and carry fixed filenames", async () => {
  const cases: Array<[() => Promise<unknown>, string]> = [
    [() => admin.call(() => getCaRoot({})), "home-ca-root.crt"],
    [() => admin.call(() => getCaIntermediate({})), "home-ca-intermediate.crt"],
    [() => admin.call(() => getCaChain({})), "home-ca-full-chain.crt"],
  ];

  for (const [call, filename] of cases) {
    const res = (await call()) as { response?: Response; data?: unknown };
    expect(res.response?.status, `GET as admin for ${filename}`).toBe(200);
    expect(res.response?.headers.get("content-type"), `${filename} content-type`).toBe("application/x-pem-file");
    expect(res.response?.headers.get("content-disposition"), `${filename} content-disposition`).toBe(
      `attachment; filename=${filename}`,
    );
    expect(await blobText(res.data), `${filename} body`).toContain("BEGIN CERTIFICATE");
  }

  // Viewer sits below the CA's admin floor; a lower role (manager, which may
  // read its own certificates' files) is the sharper boundary than public.
  const rootAsManager = await manager.call(() => getCaRoot({}));
  expect(rootAsManager.response?.status, "manager may not download the root CA").toBe(403);
  const rootAsViewer = await viewer.call(() => getCaRoot({}));
  expect(rootAsViewer.response?.status, "viewer may not download the root CA").toBe(403);
});

test("E2E-CERT-09: full-chain is the intermediate followed by the root, and nothing else", async () => {
  const chainRes = await admin.call(() => getCaChain({}));
  const chain = await blobText(chainRes.data);

  const fs = await import("node:fs/promises");
  const os = await import("node:os");
  const path = await import("node:path");
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "e2e-chain-"));
  try {
    const file = path.join(dir, "full-chain.crt");
    await fs.writeFile(file, chain);

    // A third-party parser counting the certificates is the assertion; a
    // substring count over the PEM would pass on a duplicated intermediate.
    const parsed = await run(["bash", "-lc", `openssl crl2pkcs7 -nocrl -certfile ${file} | openssl pkcs7 -print_certs -noout`]);
    const subjects = parsed.stdout.split("\n").filter((l) => l.startsWith("subject="));
    expect(subjects.length, `full-chain holds exactly two certificates; got:\n${parsed.stdout}`).toBe(2);

    const intermediateRes = await admin.call(() => getCaIntermediate({}));
    const rootRes = await admin.call(() => getCaRoot({}));
    const intermediate = await blobText(intermediateRes.data);
    const root = await blobText(rootRes.data);
    expect(chain.indexOf(intermediate.trim().split("\n")[1] ?? ""), "intermediate comes first").toBeLessThan(
      chain.indexOf(root.trim().split("\n")[1] ?? ""),
    );
  } finally {
    await fs.rm(dir, { recursive: true, force: true });
  }
});
