import {
  getAdminAboutOptions,
  getAdminIntegrityOptions,
  listAdminConsoleCommandsOptions,
  runAdminConsoleCommandMutation,
} from '@kantharos-ai/certharbor-client/react-query'
import { useMutation, useQuery } from '@tanstack/react-query'

// GET /api/v1/admin/about: system metadata plus the same preflight checks
// AdminAboutGet rendered, narrowed through systemInfoBody server-side (no
// RootCert/PasswordFile/CertsDir/UploadDir/SSLCert/SSLKey on the wire).
export function useAdminAbout() {
  return useQuery(getAdminAboutOptions())
}

// GET /api/v1/admin/integrity. Deliberately uncached client-side too: the
// backend comment on getAdminIntegrity (api/admin.go) explains why no TTL
// was warranted after measuring the real cost, so this hook does not add
// one either (no staleTime override).
export function useAdminIntegrity() {
  return useQuery(getAdminIntegrityOptions())
}

// GET /api/v1/admin/console/commands: the fixed, small allowlist of runnable
// commands. Name/Args/NativeFn never reach the wire (admin_console.go).
export function useAdminConsoleCommands() {
  return useQuery(listAdminConsoleCommandsOptions())
}

// POST /api/v1/admin/console/run. No cache to invalidate: running a command
// does not change the commands list, and about/integrity are not derived
// from console output, so this mutation carries no onSuccess invalidation.
export function useRunAdminConsoleCommand() {
  return useMutation(runAdminConsoleCommandMutation())
}
