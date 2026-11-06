import { describe, expect, it } from 'vitest'
import prefixes from '../proxy-prefixes.json' with { type: 'json' }
import { devProxy, proxiedPrefixes } from './proxy.ts'

describe('dev proxy', () => {
  it('proxies exactly the shared prefix list, with secure:false', () => {
    const proxy = devProxy('https://localhost:8443')
    expect(Object.keys(proxy)).toEqual(prefixes)
    expect(proxiedPrefixes).toEqual(prefixes)
    for (const options of Object.values(proxy))
      expect(options).toMatchObject({ secure: false, target: 'https://localhost:8443' })
  })

  it('covers the five prefixes the origin proxies', () => {
    expect([...prefixes].sort()).toEqual(['/api', '/auth', '/health', '/openapi.json', '/ready'])
  })
})
