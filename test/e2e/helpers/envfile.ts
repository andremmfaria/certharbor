import * as fs from "node:fs";
import * as path from "node:path";
import { REPO_ROOT } from "./env";

/**
 * Bootstrap scenarios edit environment keys between cases. They edit a copy
 * rather than the developer's own .env, and compose reads that copy through
 * --env-file (E2E_ENV_FILE), so a scenario never leaves the checkout modified.
 */
export function scenarioEnvFile(): string {
  return process.env.E2E_ENV_FILE ?? path.join(REPO_ROOT, ".env");
}

export function readEnvFile(file = scenarioEnvFile()): Map<string, string> {
  const out = new Map<string, string>();
  if (!fs.existsSync(file)) return out;
  for (const line of fs.readFileSync(file, "utf8").split("\n")) {
    const m = /^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/.exec(line);
    if (m && m[1] !== undefined && m[2] !== undefined) {
      out.set(m[1], m[2].trim().replace(/^["']|["']$/g, ""));
    }
  }
  return out;
}

export function getEnvKey(key: string, file = scenarioEnvFile()): string | undefined {
  return readEnvFile(file).get(key);
}

/**
 * Sets or replaces keys in place; a value of undefined comments the key out.
 * Every later line for the same key is commented out too: an .env built by
 * appending to .env.example carries a key twice, and compose reads the last.
 */
export function setEnvKeys(values: Record<string, string | undefined>, file = scenarioEnvFile()): void {
  const lines = fs.existsSync(file) ? fs.readFileSync(file, "utf8").split("\n") : [];
  const wanted = new Map(Object.entries(values));
  const remaining = new Map(wanted);

  const rewritten = lines.map((line) => {
    const m = /^\s*#?\s*([A-Za-z_][A-Za-z0-9_]*)\s*=/.exec(line);
    const key = m?.[1];
    if (!key || !wanted.has(key)) return line;
    if (!remaining.has(key)) return line.trimStart().startsWith("#") ? line : `# ${line}`;
    const value = remaining.get(key);
    remaining.delete(key);
    return value === undefined ? `# ${key}=` : `${key}=${serialise(value)}`;
  });

  for (const [key, value] of remaining) {
    if (value !== undefined) rewritten.push(`${key}=${serialise(value)}`);
  }

  fs.writeFileSync(file, rewritten.join("\n"));
}

// A multi-line value (CA_ROOT_CERT_PEM) has to survive a line-oriented file, and
// compose only preserves newlines inside a double-quoted value.
function serialise(value: string): string {
  return value.includes("\n") ? `"${value.replace(/\n/g, "\\n")}"` : value;
}
