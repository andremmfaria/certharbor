import { describe, expect, it } from 'vitest'
import { operationTagsFrom } from './operationTags.ts'

describe('operationTagsFrom', () => {
  it('groups operationIds by tag, sorted and stable', () => {
    expect(
      operationTagsFrom({
        paths: {
          '/b': { get: { operationId: 'getB', tags: ['x'] } },
          '/a': {
            get: { operationId: 'getA', tags: ['x', 'y'] },
            post: { operationId: 'postA', tags: ['y'] },
          },
          '/none': { get: { tags: ['x'] } },
        },
      }),
    ).toEqual({ x: ['getA', 'getB'], y: ['getA', 'postA'] })
  })
})
