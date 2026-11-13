import * as fs from "node:fs";
import * as path from "node:path";
import { REPO_ROOT } from "./env.js";

export type MutationMethod = "POST" | "PUT" | "PATCH" | "DELETE";
export type HTTPMethod = "GET" | MutationMethod;

export interface MutationRoute {
  method: MutationMethod;
  pattern: string;
  auth: "public" | "optional" | "required";
  role: "-" | "viewer" | "manager" | "admin";
  csrf: "yes" | "optional";
}

/** One row of backend/testdata/roles.golden, every method, unfiltered. */
export interface GoldenRoute {
  method: HTTPMethod;
  pattern: string;
  auth: "public" | "optional" | "required";
  role: "-" | "viewer" | "manager" | "admin";
  csrf: "yes" | "optional" | "no";
}

/**
 * E2E-RBAC-01 and E2E-CSRF-01 both derive their route list from this table
 * rather than from main.go: Phase 9 removed the template registrations, so
 * scraping the router there would make either sweep vacuous.
 */
export function goldenRoutes(): GoldenRoute[] {
  const golden = fs.readFileSync(path.join(REPO_ROOT, "backend", "testdata", "roles.golden"), "utf8");
  const routes: GoldenRoute[] = [];

  for (const line of golden.split(/\r?\n/)) {
    if (!line || line.startsWith("#")) continue;
    const [method, pattern, ...columns] = line.trim().split(/\s+/);
    if (!method || !pattern || !isHTTPMethod(method)) continue;
    const values = Object.fromEntries(columns.map((column) => column.split("=", 2)));
    const auth = values.auth;
    const role = values.role;
    const csrf = values.csrf;
    if (!isAuth(auth) || !isRole(role) || !isCSRF(csrf)) {
      throw new Error(`invalid role golden row: ${line}`);
    }
    routes.push({ method, pattern, auth, role, csrf });
  }

  if (routes.length === 0) {
    throw new Error("derived zero routes from backend/testdata/roles.golden; the sweep would pass vacuously");
  }
  return routes;
}

/**
 * E2E-CSRF-01 derives every API mutation which has a CSRF requirement from the
 * role matrix source of truth. Phase 9 removed the template registrations in
 * main.go, so scraping r.Post there would make the sweep vacuous.
 */
export function csrfMutationRoutes(): MutationRoute[] {
  const routes = goldenRoutes().filter(
    (route): route is GoldenRoute & { method: MutationMethod; csrf: "yes" | "optional" } =>
      isMutationMethod(route.method) && (route.csrf === "yes" || route.csrf === "optional"),
  );
  if (routes.length === 0) {
    throw new Error("derived zero CSRF mutations from backend/testdata/roles.golden; the sweep would pass vacuously");
  }
  return routes;
}

function isMutationMethod(method: string): method is MutationMethod {
  return method === "POST" || method === "PUT" || method === "PATCH" || method === "DELETE";
}

function isHTTPMethod(method: string): method is HTTPMethod {
  return method === "GET" || isMutationMethod(method);
}

function isCSRF(csrf: string | undefined): csrf is GoldenRoute["csrf"] {
  return csrf === "yes" || csrf === "optional" || csrf === "no";
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
