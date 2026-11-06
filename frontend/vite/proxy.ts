import type { ProxyOptions } from 'vite'
import prefixes from '../proxy-prefixes.json' with { type: 'json' }

// secure:false because the backend serves a leaf chained to a private root the
// host does not trust. changeOrigin keeps the Host header the backend sees
// stable regardless of the dev server's port.
export function devProxy(target: string): Record<string, ProxyOptions> {
  return Object.fromEntries(prefixes.map((prefix) => [prefix, { target, changeOrigin: true, secure: false }]))
}

export const proxiedPrefixes: readonly string[] = prefixes
