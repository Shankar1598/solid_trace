import { useEffect, useRef } from 'react'
import { bindShortcut, dispatchShortcut, placeOf, type ShortcutId } from '@/lib/shortcuts'

/**
 * Binds a handler to a shortcut while the component is mounted and the
 * handler isn't null. A handler that returns false passes the press on.
 */
export function useShortcut(id: ShortcutId, handler: (() => void | boolean) | null) {
  const latest = useRef(handler)
  latest.current = handler
  const active = handler !== null
  useEffect(() => {
    if (!active) return
    return bindShortcut(id, () => latest.current?.() !== false)
  }, [id, active])
}

/** Listens for every shortcut on the page; the dashboard layout mounts it once. */
export function useShortcutListener() {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // An editor or popup that handled the key has the last word.
      if (e.defaultPrevented || e.isComposing) return
      if (dispatchShortcut(e, placeOf(e.target as Element | null))) e.preventDefault()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
}

/** Whether the viewer is on a Mac, to show ⌘ rather than Ctrl. */
export function onMac(): boolean {
  if (typeof navigator === 'undefined') return false
  const platform = (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ?? navigator.platform
  return /mac|iphone|ipad/i.test(platform)
}
