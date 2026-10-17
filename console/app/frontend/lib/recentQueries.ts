// Recent Queries: each viewer's last Queries per Project, newest first, kept
// in their browser's localStorage under their user id, so two people on one
// browser don't share them. Storage can be missing or throw (a private
// window, blocked site data, a full quota), so every access is in try/catch
// and a failure only forgets.

/** One Query that ran, and the explorer search it ran with. */
export interface RecentQuery {
  query: string
  /** The explorer's search: its range, mode, Query and chart. */
  search: string
  /** When it ran, in epoch ms. */
  at: number
}

export const MAX_RECENT = 20

export type StorageLike = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

/** The browser's localStorage, or null where reaching it throws. */
export function browserStorage(): StorageLike | null {
  try {
    return typeof window === 'undefined' ? null : window.localStorage
  } catch {
    return null
  }
}

const keyOf = (viewer: number, org: string, project: string) => `solidtrace.recentQueries.${viewer}/${org}/${project}`
// The key of the list kept before each viewer had their own.
const sharedKeyOf = (org: string, project: string) => `solidtrace.recentQueries.${org}/${project}`

/** A viewer's recent Queries on a Project, newest first. */
export function readRecent(storage: StorageLike | null, viewer: number, org: string, project: string): RecentQuery[] {
  if (!storage) return []
  try {
    const stored: unknown = JSON.parse(storage.getItem(keyOf(viewer, org, project)) ?? takeShared(storage, viewer, org, project) ?? '[]')
    return Array.isArray(stored) ? stored.filter(isRecent) : []
  } catch {
    return []
  }
}

/**
 * The list kept before each viewer had their own, moved to the first viewer
 * who reads the Project's, as it was theirs as much as anyone's; null when
 * there is none.
 */
function takeShared(storage: StorageLike, viewer: number, org: string, project: string): string | null {
  const shared = storage.getItem(sharedKeyOf(org, project))
  if (shared === null) return null
  storage.removeItem(sharedKeyOf(org, project))
  storage.setItem(keyOf(viewer, org, project), shared)
  return shared
}

/** Records a viewer's run: its Query becomes the newest, once, and the oldest past MAX_RECENT go. */
export function recordQuery(storage: StorageLike | null, viewer: number, org: string, project: string, run: RecentQuery) {
  if (!storage) return
  const kept = readRecent(storage, viewer, org, project).filter(r => r.query !== run.query)
  try {
    storage.setItem(keyOf(viewer, org, project), JSON.stringify([run, ...kept].slice(0, MAX_RECENT)))
  } catch {
    // Storage is full or blocked: this run is not remembered.
  }
}

/** A viewer's newest `limit` recent Queries of several Projects, each with its Project's slug. */
export function latestQueries(storage: StorageLike | null, viewer: number, org: string, projects: string[], limit = MAX_RECENT) {
  return projects
    .flatMap(project => readRecent(storage, viewer, org, project).map(r => ({ project, ...r })))
    .sort((a, b) => b.at - a.at)
    .slice(0, limit)
}

function isRecent(value: unknown): value is RecentQuery {
  const r = value as RecentQuery | null
  return typeof r === 'object' && r !== null && typeof r.query === 'string' && typeof r.search === 'string' && typeof r.at === 'number'
}
