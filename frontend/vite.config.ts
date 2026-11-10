import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'
import { operationTags } from './vite/operationTags.ts'
import { devProxy } from './vite/proxy.ts'

const specPath =
  process.env.OPENAPI_SPEC ?? fileURLToPath(new URL('../backend/openapi/openapi.json', import.meta.url))

// The backend reports the sha256 of its embedded spec as contractSha, and the
// bundle carries the same digest so the SPA can tell it is talking to a
// different contract (5.10). Empty under `vite dev`, which disables the check.
function contractSha(command: 'build' | 'serve'): string {
  if (process.env.CONTRACT_SHA !== undefined) return process.env.CONTRACT_SHA
  if (command !== 'build') return ''
  return createHash('sha256').update(readFileSync(specPath)).digest('hex')
}

export default defineConfig(({ command }) => ({
  // PHASE9-DELETE: '/' once the SPA owns the root. Until then the backend's
  // server-rendered pages own every other path, and the router basename,
  // nginx's /app/ blocks and the harness all follow this one value.
  base: '/app/',
  plugins: [react(), operationTags(specPath)],
  define: {
    __CONTRACT_SHA__: JSON.stringify(contractSha(command)),
  },
  build: {
    // Under font-src 'self' a data: font is a CSP violation, so nothing inlines.
    assetsInlineLimit: 0,
    sourcemap: false,
  },
  server: {
    port: 5173,
    proxy: devProxy(process.env.DEV_BACKEND_URL ?? 'https://localhost:8443'),
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    restoreMocks: true,
    // The client's emitted imports carry no file extension, which Vite resolves
    // and Node's ESM loader does not, so it is transformed rather than externalised.
    server: { deps: { inline: [/@andremmfaria\/certharbor-client/] } },
  },
}))
