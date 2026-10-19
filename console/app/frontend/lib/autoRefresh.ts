// How often a page over a relative range runs again: a Dashboard keeps its
// choice for everyone, and the Logs explorer keeps the viewer's in the browser.
import type { StorageLike } from './recentQueries'

export type AutoRefresh = 'off' | '15s' | '30s' | '1m' | '5m'
export const AUTO_REFRESHES: AutoRefresh[] = ['off', '15s', '30s', '1m', '5m']
export const DEFAULT_AUTO_REFRESH: AutoRefresh = '15s'
export const AUTO_REFRESH_MS: Record<AutoRefresh, number | null> = { off: null, '15s': 15_000, '30s': 30_000, '1m': 60_000, '5m': 300_000 }

export const isAutoRefresh = (value: unknown): value is AutoRefresh => AUTO_REFRESHES.includes(value as AutoRefresh)

const LOGS_KEY = 'solidtrace.logsAutoRefresh'

/** The explorer's auto-refresh in this browser: 15s until the viewer picks another. */
export function readLogsAutoRefresh(storage: StorageLike | null): AutoRefresh {
  try {
    const value = storage?.getItem(LOGS_KEY)
    return isAutoRefresh(value) ? value : DEFAULT_AUTO_REFRESH
  } catch {
    return DEFAULT_AUTO_REFRESH
  }
}

export function saveLogsAutoRefresh(storage: StorageLike | null, value: AutoRefresh) {
  try {
    storage?.setItem(LOGS_KEY, value)
  } catch {
    // Storage is full or blocked: the choice lasts until the page closes.
  }
}
