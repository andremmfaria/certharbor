import { afterEach, describe, expect, it, vi } from 'vitest'
import { downloadBlob } from './downloadBlob'

describe('downloadBlob', () => {
  afterEach(() => vi.useRealTimers())

  it('downloads under the given filename and releases the object URL', () => {
    vi.useFakeTimers()
    const create = vi.fn(() => 'blob:fake')
    const revoke = vi.fn()
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke }))
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      expect(this.download).toBe('leaf.pem')
      expect(this.href).toBe('blob:fake')
    })

    downloadBlob(new Blob(['x']), 'leaf.pem')

    expect(click).toHaveBeenCalledTimes(1)
    expect(document.querySelector('a[download]')).toBeNull()
    vi.runAllTimers()
    expect(revoke).toHaveBeenCalledWith('blob:fake')
  })
})
