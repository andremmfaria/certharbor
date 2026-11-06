import { describe, expect, it } from 'vitest'
import { checkSurface, importedClientNames, operationIdsOf, parseAllowlist } from './sdk-surface.mjs'

describe('sdk surface', () => {
  const operationIds = ['getSession', 'getStatus', 'createSession']

  it('reads names imported from any client entry point and ignores everything else', () => {
    const names = importedClientNames(`
      import { getSession as gs } from '@andremmfaria/step-ca-webui-client'
      import { createSessionMutation } from '@andremmfaria/step-ca-webui-client/react-query'
      import { getStatus } from './local'
    `)
    expect([...names].sort()).toEqual(['createSessionMutation', 'getSession'])
  })

  it('treats the react-query helpers as a use of their operation', () => {
    const { dead } = checkSurface({
      operationIds,
      imported: new Set(['getSessionOptions', 'createSessionMutation']),
      allowlist: ['getStatus'],
    })
    expect(dead).toEqual([])
  })

  it('reports an operation nothing imports and nobody allowlisted', () => {
    const { dead } = checkSurface({ operationIds, imported: new Set(['getSession']), allowlist: [] })
    expect(dead).toEqual(['getStatus', 'createSession'])
  })

  it('reports an allowlist entry that is now used or no longer exists', () => {
    const { staleAllowed } = checkSurface({
      operationIds,
      imported: new Set(['getSession', 'createSession']),
      allowlist: ['getSession', 'getStatus', 'removedOp'],
    })
    expect(staleAllowed).toEqual(['getSession', 'removedOp'])
  })

  it('parses an allowlist with comments and blank lines', () => {
    expect(parseAllowlist('# why\n\ngetStatus\n  postX  \n')).toEqual(['getStatus', 'postX'])
  })

  it('lists the operations of a spec', () => {
    expect(operationIdsOf({ paths: { '/a': { get: { operationId: 'getA' }, post: {} } } })).toEqual(['getA'])
  })
})
