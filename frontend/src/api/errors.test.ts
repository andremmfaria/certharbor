import { describe, expect, it } from 'vitest'
import { errorMessage, errorStatus, hasProblemType, isClientError, isNetworkOrServerError } from './errors'

describe('error classification', () => {
  it('reads the status off a problem document', () => {
    expect(errorStatus({ status: 503 })).toBe(503)
    expect(errorStatus('<html>Bad Gateway</html>')).toBeUndefined()
    expect(errorStatus(new TypeError('Failed to fetch'))).toBeUndefined()
  })

  it('treats only 4xx as a client error', () => {
    expect(isClientError({ status: 401 })).toBe(true)
    expect(isClientError({ status: 503 })).toBe(false)
    expect(isClientError(new TypeError('Failed to fetch'))).toBe(false)
    expect(isClientError('<html/>')).toBe(false)
  })

  it('treats a refused connection, an HTML error page and a 5xx as retryable', () => {
    expect(isNetworkOrServerError(new TypeError('Failed to fetch'))).toBe(true)
    expect(isNetworkOrServerError('<html/>')).toBe(true)
    expect(isNetworkOrServerError({ status: 502 })).toBe(true)
    expect(isNetworkOrServerError({ status: 400 })).toBe(false)
  })

  it('matches a problem type by its final segment', () => {
    expect(
      hasProblemType({ type: 'https://certharbor/errors/upstream-unavailable' }, 'upstream-unavailable'),
    ).toBe(true)
    expect(hasProblemType({ type: 'about:blank' }, 'csrf')).toBe(false)
  })

  it('prefers detail, then title, then the fallback', () => {
    expect(errorMessage({ detail: 'd', title: 't' })).toBe('d')
    expect(errorMessage({ title: 't' })).toBe('t')
    expect(errorMessage('x', 'fallback')).toBe('fallback')
  })
})
