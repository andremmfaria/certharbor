import type { ErrorModel } from '@kantharos-ai/certharbor-client'

export type Problem = ErrorModel

export const PROBLEM_TYPE_BASE = 'https://certharbor/errors/'

// The generated client throws the parsed error body, not the response, so the
// status travels inside the problem document. A network failure is a
// TypeError and an HTML error page from a proxy is a bare string: neither
// carries a status.
export function isProblem(e: unknown): e is Problem {
  return typeof e === 'object' && e !== null && !(e instanceof Error)
}

export function errorStatus(e: unknown): number | undefined {
  if (!isProblem(e)) return undefined
  return typeof e.status === 'number' ? e.status : undefined
}

export function isClientError(e: unknown): boolean {
  const status = errorStatus(e)
  return status !== undefined && status >= 400 && status < 500
}

export function isNetworkOrServerError(e: unknown): boolean {
  return !isClientError(e)
}

export function hasProblemType(e: unknown, suffix: string): boolean {
  return isProblem(e) && typeof e.type === 'string' && e.type.endsWith('/' + suffix)
}

export function errorMessage(e: unknown, fallback = 'Something went wrong.'): string {
  if (isProblem(e)) return e.detail || e.title || fallback
  return fallback
}
