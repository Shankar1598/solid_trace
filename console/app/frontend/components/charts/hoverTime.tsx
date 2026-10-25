import { createContext, useContext, useMemo, useSyncExternalStore } from 'react'

/** The time under the pointer on one time chart, and which chart it is over. */
export interface HoverTime {
  time: number
  source: string
}

interface HoverTimeStore {
  get: () => HoverTime | null
  set: (next: HoverTime | null) => void
  subscribe: (listener: () => void) => () => void
}

const HoverTimeContext = createContext<HoverTimeStore | null>(null)

/**
 * Shares the time under the pointer among the time charts inside it, such as
 * a Dashboard's, so each of the others marks that time with a line.
 */
export function HoverTimeProvider({ children }: { children: React.ReactNode }) {
  const store = useMemo<HoverTimeStore>(() => {
    let current: HoverTime | null = null
    const listeners = new Set<() => void>()
    return {
      get: () => current,
      set: next => {
        if (next?.time === current?.time && next?.source === current?.source) return
        current = next
        listeners.forEach(l => l())
      },
      subscribe: listener => {
        listeners.add(listener)
        return () => listeners.delete(listener)
      },
    }
  }, [])
  return <HoverTimeContext.Provider value={store}>{children}</HoverTimeContext.Provider>
}

const noSubscribe = () => () => {}

/**
 * The time another chart's pointer is over, for `source` to mark, and a
 * setter for the time over `source` itself; null and a no-op outside a
 * HoverTimeProvider.
 */
export function useHoverTime(source: string): [number | null, (time: number | null) => void] {
  const store = useContext(HoverTimeContext)
  // A number, not the shared object, so a chart re-renders only when the time it marks changes.
  const marked = useSyncExternalStore(store?.subscribe ?? noSubscribe, () => {
    const hover = store?.get()
    return hover && hover.source !== source ? hover.time : null
  })
  const set = useMemo(
    () => (time: number | null) => {
      if (!store) return
      // Leaving a chart clears only its own time, not one another chart set since.
      if (time === null) {
        if (store.get()?.source === source) store.set(null)
      } else store.set({ time, source })
    },
    [store, source],
  )
  return [marked, set]
}
