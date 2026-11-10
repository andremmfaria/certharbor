// Prefixed first: under SESSION_SECURE=true a bare-named cookie cannot have
// been set by this server, so the prefixed one winning the probe means a
// sibling host cannot shadow it. Under false the prefixed name cannot exist.
export const CSRF_COOKIE_NAMES = ['__Host-certharbor-csrf', 'certharbor-csrf'] as const

export function readCookie(name: string, cookies: string = document.cookie): string | undefined {
  for (const part of cookies.split(';')) {
    const eq = part.indexOf('=')
    if (eq === -1) continue
    if (part.slice(0, eq).trim() !== name) continue
    const value = part.slice(eq + 1).trim()
    try {
      return decodeURIComponent(value)
    } catch {
      return value
    }
  }
  return undefined
}

export function readCsrf(cookies?: string): string | undefined {
  return CSRF_COOKIE_NAMES.map((name) => readCookie(name, cookies)).find(Boolean)
}
