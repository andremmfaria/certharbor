import { describe, expect, it } from 'vitest'
import { z } from 'zod'
import mainSource from './main.tsx?raw'
import './zodConfig'

describe('zodConfig', () => {
  it('turns off the eval probe that strict CSP reports', () => {
    expect(z.config().jitless).toBe(true)
  })

  it('is the first module main.tsx evaluates, before any schema exists', () => {
    expect(mainSource.split('\n')[0]).toBe("import './zodConfig'")
  })
})
