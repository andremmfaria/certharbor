import { readFileSync } from 'node:fs'
import type { Plugin } from 'vite'

const VIRTUAL_ID = 'virtual:operation-tags'
const RESOLVED_ID = '\0' + VIRTUAL_ID

interface Spec {
  paths: Record<string, Record<string, { operationId?: string; tags?: string[] } | undefined>>
}

export function operationTagsFrom(spec: Spec): Record<string, string[]> {
  const byTag: Record<string, string[]> = {}
  for (const item of Object.values(spec.paths)) {
    for (const op of Object.values(item)) {
      if (!op?.operationId) continue
      for (const tag of op.tags ?? []) {
        ;(byTag[tag] ??= []).push(op.operationId)
      }
    }
  }
  for (const ids of Object.values(byTag)) ids.sort()
  return Object.fromEntries(Object.entries(byTag).sort(([a], [b]) => a.localeCompare(b)))
}

// Exposes the spec's tag to operationId mapping to invalidation.ts, so the map
// the SPA invalidates by is derived from the contract rather than retyped.
export function operationTags(specPath: string): Plugin {
  return {
    name: 'operation-tags',
    resolveId(id) {
      return id === VIRTUAL_ID ? RESOLVED_ID : undefined
    },
    load(id) {
      if (id !== RESOLVED_ID) return undefined
      this.addWatchFile(specPath)
      const spec = JSON.parse(readFileSync(specPath, 'utf8')) as Spec
      return `export default ${JSON.stringify(operationTagsFrom(spec))}`
    },
  }
}
