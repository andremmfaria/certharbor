import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";

// "type": "module" in package.json means this file has no CJS __dirname.
const __dirname = path.dirname(fileURLToPath(import.meta.url));

// The repo root is bind-mounted into the harness container, so the compose
// file, .env and secrets/* are all reachable from one place.
export const REPO_ROOT = process.env.E2E_REPO_ROOT ?? path.resolve(__dirname, "..", "..", "..");

/**
 * The origin under test: nginx, the only service a browser reaches. Its name is
 * also the SNI it accepts, so WEB_HOSTNAME must be certharbor-console for an
 * in-network run.
 */
export const BASE_URL = process.env.BASE_URL ?? "https://certharbor-console:8443";

/**
 * The backend directly, for the tests that must bypass the origin: the health
 * tier and the direct half of the forgery table. The infra tier runs on the
 * host, where this name does not resolve, and uses backendAddress() instead.
 */
export const BACKEND_URL = process.env.BACKEND_URL ?? "https://certharbor-control:8443";

/** PHASE9-DELETE: "" once the SPA owns the root. The SPA's mount point under the origin, Vite's base without the trailing slash. */
export const SPA_BASE = "/app";

/**
 * Host-context runs dial nginx's published port, not the service name.
 * UI_HTTPS_PORT lives only in the (possibly scenario-edited) .env file that
 * compose reads via --env-file, never in the harness's own environment, so
 * this has to parse the file rather than read process.env.
 */
export function hostURL(): string {
  if (process.env.HOST_URL) return process.env.HOST_URL;
  const envFile = process.env.E2E_ENV_FILE ?? path.join(REPO_ROOT, ".env");
  let port = "443";
  if (fs.existsSync(envFile)) {
    const m = /^\s*UI_HTTPS_PORT\s*=\s*(.*)$/m.exec(fs.readFileSync(envFile, "utf8"));
    if (m && m[1] !== undefined) port = m[1].trim().replace(/^["']|["']$/g, "");
  }
  return `https://localhost:${port}`;
}

export const ADMIN_USERNAME = process.env.E2E_ADMIN_USERNAME ?? "admin";

export const FLAGS = {
  oidc: process.env.E2E_OIDC_ENABLED === "1",
  mail: process.env.E2E_MAIL_ENABLED === "1",
  le: process.env.E2E_LE_ENABLED === "1",
};

/** Reads a file the compose stack also reads, trimming the trailing newline the way getEnvOrFile does. */
export function readSecretFile(name: string): string {
  const p = path.join(REPO_ROOT, "secrets", name);
  return fs.readFileSync(p, "utf8").replace(/[\r\n]+$/, "");
}

/**
 * The admin password is read off the mounted .env rather than passed as its own
 * environment variable, which is one fewer value to keep in sync with the
 * workflow's ::add-mask:: list (Section 2.7.2).
 */
export function adminPassword(): string {
  if (process.env.CERTHARBOR_ADMIN_PASSWORD) return process.env.CERTHARBOR_ADMIN_PASSWORD;
  const envFile = path.join(REPO_ROOT, ".env");
  const raw = fs.readFileSync(envFile, "utf8");
  for (const line of raw.split("\n")) {
    const m = /^\s*CERTHARBOR_ADMIN_PASSWORD\s*=\s*(.*)$/.exec(line);
    if (m && m[1] !== undefined) {
      return m[1].trim().replace(/^["']|["']$/g, "");
    }
  }
  throw new Error("CERTHARBOR_ADMIN_PASSWORD is set neither in the environment nor in .env, startup would have fatalled");
}

/** The canary values E2E-SEC-04 sweeps collected artifacts for. */
export function secretCanaries(): Record<string, string> {
  return {
    ca_password: readSecretFile("ca_password"),
    secret_key: readSecretFile("secret_key"),
    postgres_password: readSecretFile("postgres_password"),
    admin_password: adminPassword(),
  };
}
