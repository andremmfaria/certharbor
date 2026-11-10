import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

const CLIENT_PACKAGE = '@andremmfaria/certharbor-client'
const DERIVED_SUFFIXES = ['', 'Options', 'QueryKey', 'Mutation', 'InfiniteOptions', 'InfiniteQueryKey']

/** @param {string} dir @returns {string[]} */
function sourceFiles(dir) {
  const out = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) {
      if (entry.name !== 'test') out.push(...sourceFiles(path))
    } else if (/\.(ts|tsx)$/.test(entry.name) && !/\.test\.(ts|tsx)$/.test(entry.name)) {
      out.push(path)
    }
  }
  return out
}

/** Names imported from the generated client package, by any of its entry points. */
export function importedClientNames(
  /** @type {string} */ source,
  /** @type {string} */ fileName = 'file.tsx',
) {
  const names = new Set()
  const file = ts.createSourceFile(fileName, source, ts.ScriptTarget.Latest, true)
  file.forEachChild((node) => {
    if (!ts.isImportDeclaration(node) || !ts.isStringLiteral(node.moduleSpecifier)) return
    if (!node.moduleSpecifier.text.startsWith(CLIENT_PACKAGE)) return
    const bindings = node.importClause?.namedBindings
    if (bindings && ts.isNamedImports(bindings)) {
      for (const element of bindings.elements) names.add((element.propertyName ?? element.name).text)
    }
  })
  return names
}

export function operationIdsOf(
  /** @type {{ paths: Record<string, Record<string, { operationId?: string } | undefined>> }} */ spec,
) {
  const ids = []
  for (const item of Object.values(spec.paths)) {
    for (const op of Object.values(item)) if (op?.operationId) ids.push(op.operationId)
  }
  return ids.sort()
}

export function parseAllowlist(/** @type {string} */ text) {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith('#'))
}

/**
 * Dead surface is a spec operation whose generated function nothing imports,
 * unless a reviewer wrote it down. An allowlist entry that names an operation
 * which is now used, or no longer exists, is stale and fails too, so the list
 * cannot quietly outlive its reason.
 *
 * @param {{ operationIds: string[], imported: Set<string>, allowlist: string[] }} input
 */
export function checkSurface({ operationIds, imported, allowlist }) {
  // hey-api's camelCase transform collapses a trailing run of capitals (e.g.
  // the "CA" in operationId "getCA") into a single title-cased word, so the
  // generated export is `getCa`, not `getCA`. Spec operationIds are compared
  // case-insensitively against imports for this reason: it is a casing
  // normalization, not a weakening of what "used" means.
  const importedLower = new Set([...imported].map((name) => name.toLowerCase()))
  const isUsed = (/** @type {string} */ id) =>
    DERIVED_SUFFIXES.some((suffix) => importedLower.has((id + suffix).toLowerCase()))
  const allowed = new Set(allowlist)
  const known = new Set(operationIds)
  return {
    dead: operationIds.filter((id) => !isUsed(id) && !allowed.has(id)),
    staleAllowed: allowlist.filter((id) => !known.has(id) || isUsed(id)),
  }
}

function main() {
  const root = fileURLToPath(new URL('..', import.meta.url))
  const specPath = process.env.OPENAPI_SPEC ?? join(root, '../backend/openapi/openapi.json')
  const allowlistPath = join(root, '../clients/ts/unused-allowlist.txt')
  const allowlist = existsSync(allowlistPath) ? parseAllowlist(readFileSync(allowlistPath, 'utf8')) : []

  const imported = new Set()
  for (const file of sourceFiles(join(root, 'src'))) {
    for (const name of importedClientNames(readFileSync(file, 'utf8'), file)) imported.add(name)
  }

  const { dead, staleAllowed } = checkSurface({
    operationIds: operationIdsOf(JSON.parse(readFileSync(specPath, 'utf8'))),
    imported,
    allowlist,
  })

  for (const id of dead) {
    console.error(
      `dead API surface: ${id} is in the spec, imported nowhere in frontend/src, and not in ${relative(root, allowlistPath)}`,
    )
  }
  for (const id of staleAllowed) {
    console.error(
      `stale allowlist entry: ${id} is used or no longer exists, remove it from ${relative(root, allowlistPath)}`,
    )
  }
  if (dead.length > 0 || staleAllowed.length > 0) process.exit(1)
  console.log('every spec operation is imported or allowlisted')
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main()
