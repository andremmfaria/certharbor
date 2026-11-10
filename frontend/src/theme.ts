export const THEME_PREFERENCES = ['auto', 'dark', 'light', 'blue'] as const
export type ThemePreference = (typeof THEME_PREFERENCES)[number]
export type ResolvedTheme = 'dark' | 'light' | 'blue'

// The same key the inline script in index.html reads before first paint.
export const THEME_STORAGE_KEY = 'certharbor-theme'

export function isThemePreference(value: unknown): value is ThemePreference {
  return typeof value === 'string' && (THEME_PREFERENCES as readonly string[]).includes(value)
}

export function resolveTheme(preference: ThemePreference, prefersLight: boolean): ResolvedTheme {
  if (preference === 'auto') return prefersLight ? 'light' : 'dark'
  return preference
}

export function storedThemePreference(storage: Pick<Storage, 'getItem'> = localStorage): ThemePreference {
  try {
    const value = storage.getItem(THEME_STORAGE_KEY)
    return isThemePreference(value) ? value : 'auto'
  } catch {
    return 'auto'
  }
}

// `auto` resolves against the browser rather than degrading to dark as the
// server-rendered UI did.
export function applyTheme(preference: ThemePreference, root: HTMLElement = document.documentElement): void {
  const prefersLight = window.matchMedia('(prefers-color-scheme: light)').matches
  root.setAttribute('data-theme', resolveTheme(preference, prefersLight))
  try {
    localStorage.setItem(THEME_STORAGE_KEY, preference)
  } catch {
    // Storage can be disabled; the theme still applies for this page load.
  }
}

// Keeps `auto` following the operating system while the page is open.
export function watchSystemTheme(): () => void {
  const query = window.matchMedia('(prefers-color-scheme: light)')
  const onChange = () => {
    if (storedThemePreference() === 'auto') {
      document.documentElement.setAttribute('data-theme', resolveTheme('auto', query.matches))
    }
  }
  query.addEventListener('change', onChange)
  return () => query.removeEventListener('change', onChange)
}
