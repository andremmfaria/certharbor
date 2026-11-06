import { afterEach, describe, expect, it, vi } from 'vitest'
import { applyTheme, resolveTheme, storedThemePreference, THEME_STORAGE_KEY } from './theme'

function stubMedia(light: boolean) {
  vi.stubGlobal('matchMedia', () => ({
    matches: light,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
}

describe('theme', () => {
  afterEach(() => {
    localStorage.clear()
    document.documentElement.removeAttribute('data-theme')
  })

  it('resolves auto against the browser rather than degrading to dark', () => {
    expect(resolveTheme('auto', true)).toBe('light')
    expect(resolveTheme('auto', false)).toBe('dark')
    expect(resolveTheme('blue', true)).toBe('blue')
  })

  it('applies and persists a preference under the key the inline script reads', () => {
    stubMedia(false)
    applyTheme('light')
    expect(document.documentElement.getAttribute('data-theme')).toBe('light')
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light')
  })

  it('falls back to auto for a stored value it does not recognise', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'neon')
    expect(storedThemePreference()).toBe('auto')
  })
})
