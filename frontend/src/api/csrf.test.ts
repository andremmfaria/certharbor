import { describe, expect, it } from 'vitest'
import { CSRF_COOKIE_NAMES, readCookie, readCsrf } from './csrf'

describe('readCsrf', () => {
  it('reads the prefixed cookie when SESSION_SECURE is on', () => {
    expect(readCsrf('__Host-certharbor-csrf=secure-token; theme=dark')).toBe('secure-token')
  })

  it('reads the bare cookie when SESSION_SECURE is off', () => {
    expect(readCsrf('theme=dark; certharbor-csrf=bare-token')).toBe('bare-token')
  })

  it('prefers the prefixed cookie when both exist', () => {
    expect(readCsrf('certharbor-csrf=tossed; __Host-certharbor-csrf=real')).toBe('real')
  })

  it('returns undefined when neither exists', () => {
    expect(readCsrf('theme=dark')).toBeUndefined()
    expect(readCsrf('')).toBeUndefined()
  })

  it('does not match a cookie whose name merely ends with a probed name', () => {
    expect(readCsrf('evil-certharbor-csrf=x')).toBeUndefined()
  })

  it('probes the prefixed name first', () => {
    expect(CSRF_COOKIE_NAMES[0]).toBe('__Host-certharbor-csrf')
  })

  it('reads document.cookie by default', () => {
    document.cookie = 'certharbor-csrf=from-document'
    expect(readCsrf()).toBe('from-document')
    document.cookie = 'certharbor-csrf=; Max-Age=0'
  })
})

describe('readCookie', () => {
  it('decodes percent-encoded values and tolerates malformed ones', () => {
    expect(readCookie('a', 'a=b%20c')).toBe('b c')
    expect(readCookie('a', 'a=%E0%A4%A')).toBe('%E0%A4%A')
  })
})
