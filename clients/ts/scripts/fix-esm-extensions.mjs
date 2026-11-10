#!/usr/bin/env node
// tsc with moduleResolution "Bundler" emits relative import/export specifiers
// with no file extension (e.g. `from "./sdk.gen"`, `from "./client"`), which
// bundlers (Vite, the frontend's consumer) resolve fine but Node's native ESM
// loader does not -- every relative specifier needs an explicit extension,
// and a directory import needs its index file spelled out too. The frontend
// never hit this because it never runs the package's dist/ output directly
// through Node; test/e2e does. Rather than switch moduleResolution (which
// would also require every relative import in src/ to already carry an
// extension, something openapi-ts's generator does not emit), this postbuild
// step rewrites the already-compiled output in place, resolving each bare
// specifier against the real filesystem rather than guessing from its text.
import { existsSync, readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

const DIST = new URL("../dist", import.meta.url).pathname;

const SPECIFIER = /((?:from|import)\s*\(?\s*["'])(\.[^"']+)(["'])/g;

function walk(dir) {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      walk(full);
    } else if (full.endsWith(".js")) {
      fixFile(full);
    }
  }
}

function resolveSpecifier(fileDir, spec) {
  const asFile = join(fileDir, `${spec}.js`);
  if (existsSync(asFile)) return `${spec}.js`;
  const asIndex = join(fileDir, spec, "index.js");
  if (existsSync(asIndex)) return `${spec}/index.js`;
  // Already has a real extension, or genuinely unresolvable -- leave as-is
  // rather than guess; a later import failure is easier to diagnose than a
  // silently wrong rewrite.
  return spec;
}

function fixFile(file) {
  const fileDir = dirname(file);
  const original = readFileSync(file, "utf8");
  const fixed = original.replace(SPECIFIER, (match, prefix, spec, quote) => {
    const resolved = resolveSpecifier(fileDir, spec);
    if (resolved === spec) return match;
    return `${prefix}${resolved}${quote}`;
  });
  if (fixed !== original) {
    writeFileSync(file, fixed);
  }
}

walk(DIST);
