// The Sections a viewer folded on a Dashboard, kept in their browser's
// localStorage per Dashboard. Only that viewer sees them folded; the server
// never knows. Where the storage can't be read or written, every Section is
// unfolded and the page works as before.
import type { StorageLike } from '../recentQueries'

const keyOf = (dashboardId: number) => `solidtrace.foldedSections.${dashboardId}`

/** The ids of the Sections folded on the Dashboard. */
export function readFolded(storage: StorageLike | null, dashboardId: number): Set<number> {
  if (!storage) return new Set()
  try {
    const stored: unknown = JSON.parse(storage.getItem(keyOf(dashboardId)) ?? '[]')
    return new Set(Array.isArray(stored) ? stored.filter((id): id is number => Number.isInteger(id)) : [])
  } catch {
    return new Set()
  }
}

/** Keeps the ids of the Sections folded on the Dashboard; none removes the key. */
export function writeFolded(storage: StorageLike | null, dashboardId: number, folded: ReadonlySet<number>) {
  if (!storage) return
  try {
    if (folded.size === 0) storage.removeItem(keyOf(dashboardId))
    else storage.setItem(keyOf(dashboardId), JSON.stringify([...folded]))
  } catch {
    // A full or blocked storage keeps the fold for this page only.
  }
}
