import { describe, expect, it, vi } from 'vitest'
import { installPreloadErrorReload } from './preloadError'

function setup(now: { t: number }) {
  const listeners: ((e: Event) => void)[] = []
  const store = new Map<string, string>()
  const reload = vi.fn()
  installPreloadErrorReload(
    {
      addEventListener: (_type: string, l: EventListenerOrEventListenerObject) =>
        listeners.push(l as (e: Event) => void),
    },
    { getItem: (k) => store.get(k) ?? null, setItem: (k, v) => void store.set(k, v) },
    reload,
    () => now.t,
  )
  const fire = () => {
    const event = new Event('vite:preloadError', { cancelable: true })
    listeners.forEach((l) => l(event))
    return event
  }
  return { fire, reload }
}

describe('vite:preloadError', () => {
  it('reloads once, and not again inside the interval', () => {
    const now = { t: 1_000_000 }
    const { fire, reload } = setup(now)

    expect(fire().defaultPrevented).toBe(true)
    expect(reload).toHaveBeenCalledTimes(1)

    now.t += 5_000
    fire()
    expect(reload).toHaveBeenCalledTimes(1)

    now.t += 60_000
    fire()
    expect(reload).toHaveBeenCalledTimes(2)
  })
})
