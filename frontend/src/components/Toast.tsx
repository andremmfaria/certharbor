import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'

type ToastKind = 'ok' | 'err'

interface ToastItem {
  id: number
  kind: ToastKind
  text: string
}

interface ToastApi {
  success: (text: string) => void
  error: (text: string) => void
}

const ToastContext = createContext<ToastApi | null>(null)

const DISMISS_AFTER_MS = 5000

// Hand-rolled because toast libraries inject a runtime <style> tag, which
// style-src 'self' blocks (D12.2). It renders against the ported .alert rules.
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const nextId = useRef(1)

  const dismiss = useCallback((id: number) => setItems((cur) => cur.filter((t) => t.id !== id)), [])

  const push = useCallback(
    (kind: ToastKind, text: string) => {
      const id = nextId.current++
      setItems((cur) => [...cur, { id, kind, text }])
      setTimeout(() => dismiss(id), DISMISS_AFTER_MS)
    },
    [dismiss],
  )

  const api = useMemo<ToastApi>(
    () => ({ success: (text) => push('ok', text), error: (text) => push('err', text) }),
    [push],
  )

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="toast-region" aria-live="polite" data-testid="toast-region">
        {items.map((t) => (
          <div
            key={t.id}
            className={`alert alert-${t.kind} toast`}
            role={t.kind === 'err' ? 'alert' : 'status'}
          >
            <span>{t.text}</span>
            <button type="button" className="toast-close" aria-label="Dismiss" onClick={() => dismiss(t.id)}>
              ×
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast(): ToastApi {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used inside a ToastProvider')
  return ctx
}
