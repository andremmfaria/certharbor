const STORAGE_KEY = 'step-webui-preload-reload'
const MIN_INTERVAL_MS = 30_000

// A frontend deploy replaces content-hashed chunks wholesale, so a tab opened
// before it fails the dynamic import of a lazy route (5.10). One reload picks
// up the new index.html, and the interval stops a chunk that is genuinely
// missing from turning that into a reload loop.
export function installPreloadErrorReload(
  target: Pick<Window, 'addEventListener'> = window,
  storage: Pick<Storage, 'getItem' | 'setItem'> = sessionStorage,
  reload: () => void = () => window.location.reload(),
  now: () => number = Date.now,
): void {
  target.addEventListener('vite:preloadError', (event) => {
    const last = Number(storage.getItem(STORAGE_KEY) ?? 0)
    if (now() - last < MIN_INTERVAL_MS) return
    event.preventDefault()
    storage.setItem(STORAGE_KEY, String(now()))
    reload()
  })
}
