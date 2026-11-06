import { describe, expect, it } from 'vitest'
import { problemToFieldErrors } from './problemToFieldErrors'

describe('problemToFieldErrors', () => {
  it('maps body locations to form field paths', () => {
    expect(
      problemToFieldErrors({
        status: 422,
        errors: [
          { location: 'body.name', message: 'expected string' },
          { location: 'body.items[3].tags', message: 'too long' },
        ],
      }),
    ).toEqual({ name: 'expected string', 'items.3.tags': 'too long' })
  })

  it('skips locations that are not form fields, and keeps the first message per field', () => {
    expect(
      problemToFieldErrors({
        errors: [
          { location: 'query.page', message: 'bad' },
          { location: 'body.name', message: 'first' },
          { location: 'body.name', message: 'second' },
          { message: 'no location' },
        ],
      }),
    ).toEqual({ name: 'first' })
  })

  it('returns nothing for a problem without details', () => {
    expect(problemToFieldErrors({ status: 401 })).toEqual({})
    expect(problemToFieldErrors({ errors: null })).toEqual({})
  })
})
