import * as fs from "node:fs";
import * as path from "node:path";
import { REPO_ROOT } from "./env.js";

export type MutationMethod = "POST" | "PUT" | "PATCH" | "DELETE";

export interface MutationRoute {
  method: MutationMethod;
  pattern: string;
  auth: "public" | "optional" | "required";
  role: "-" | "viewer" | "manager" | "admin";
  csrf: "yes" | "optional";
}

/**
 * E2E-CSRF-01 derives every API mutation which has a CSRF requirement from the
 * role matrix source of truth. Phase 9 removed the template registrations in
 * main.go, so scraping r.Post there would make the sweep vacuous.
 */
export function csrfMutationRoutes(): MutationRoute[] {
  const golden = fs.readFileSync(path.join(REPO_ROOT, "backend", "testdata", "roles.golden"), "utf8");
  const routes: MutationRoute[] = [];

  for (const line of golden.split(/\r?\n/)) {
    if (!line || line.startsWith("#")) continue;
    const [method, pattern, ...columns] = line.trim().split(/\s+/);
    if (!method || !pattern || !isMutationMethod(method)) continue;
    const values = Object.fromEntries(columns.map((column) => column.split("=", 2)));
    const csrf = values.csrf;
    if (csrf !== "yes" && csrf !== "optional") continue;
    const auth = values.auth;
    const role = values.role;
    if (!isAuth(auth) || !isRole(role)) {
      throw new Error(`invalid role golden row: ${line}`);
    }
    routes.push({ method, pattern, auth, role, csrf });
  }

  if (routes.length === 0) {
    throw new Error("derived zero CSRF mutations from backend/testdata/roles.golden; the sweep would pass vacuously");
  }
  return routes;
}

function isMutationMethod(method: string): method is MutationMethod {
  return method === "POST" || method === "PUT" || method === "PATCH" || method === "DELETE";
}

function isAuth(auth: string | undefined): auth is MutationRoute["auth"] {
  return auth === "public" || auth === "optional" || auth === "required";
}

function isRole(role: string | undefined): role is MutationRoute["role"] {
  return role === "-" || role === "viewer" || role === "manager" || role === "admin";
}

/** Substitutes a deliberately nonexistent id for a Huma path placeholder. */
export function withID(pattern: string, id: string | number): string {
  return pattern.replace(/\{[^}]+\}/g, String(id));
}
