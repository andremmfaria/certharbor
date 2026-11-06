// The callback redirects to /login?error=<code> with a code from a closed set.
// The text lives here, never in the URL, so nothing server-supplied is ever
// rendered from a query parameter (5.3).
const MESSAGES: Record<string, string> = {
  denied: 'Single sign-on was cancelled or refused.',
  expired: 'The single sign-on attempt took too long. Please try again.',
  disabled: 'This account is disabled. Please contact your administrator.',
}

const GENERIC = 'Single sign-on failed. Please try again.'

export function oidcErrorText(code: string | null): string | undefined {
  if (!code) return undefined
  return Object.hasOwn(MESSAGES, code) ? MESSAGES[code] : GENERIC
}
