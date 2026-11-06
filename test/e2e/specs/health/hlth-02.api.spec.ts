import { BACKEND_URL } from "../../helpers/env";
import { newJar } from "../../helpers/session";
import { expect, test } from "../../fixtures/test";

// Oracle pair with E2E-HLTH-03: on its own this passes against a /ready that
// always answers ready, which is why the pair must never be split in triage.
test("E2E-HLTH-02: /ready with everything healthy", async () => {
  const anon = await newJar(BACKEND_URL);
  try {
    const res = await anon.get("/ready");
    expect(res.status()).toBe(200);
    // Exactly these keys: both leaves are stepca-issued on the e2e stack, and
    // a key appearing here unannounced is a contract change.
    const body = (await res.json()) as Record<string, string>;
    expect(Object.keys(body).sort()).toEqual(["backendCert", "backendCertNotAfter", "status", "webCert", "webCertNotAfter"]);
    expect(body).toMatchObject({ status: "ready", backendCert: "ok", webCert: "ok" });
    for (const key of ["backendCertNotAfter", "webCertNotAfter"]) {
      expect(Date.parse(body[key] ?? ""), `${key} is an RFC 3339 time in the future`).toBeGreaterThan(Date.now());
    }
  } finally {
    await anon.dispose();
  }
});
