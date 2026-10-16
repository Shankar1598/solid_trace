// Recent Queries: each viewer's last Queries per Project, newest first, kept
// in their browser's localStorage. Storage can be missing or throw (a private
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

export type StorageLike = Pick<Storage, 'getItem' | 'setItem'>

/** The browser's localStorage, or null where reaching it throws. */
export function browserStorage(): StorageLike | null {
  try {
    return typeof window === 'undefined' ? null : window.localStorage
  } catch {
    return null
  }
}

const keyOf = (org: string, project: string) => `solidtrace.recentQueries.${org}/${project}`

/** A Project's recent Queries, newest first. */
export function readRecent(storage: StorageLike | null, org: string, project: string): RecentQuery[] {
  if (!storage) return []
  try {
    const stored: unknown = JSON.parse(storage.getItem(keyOf(org, project)) ?? '[]')
    return Array.isArray(stored) ? stored.filter(isRecent) : []
  } catch {
    return []
  }
}

/** Records a run: its Query becomes the newest, once, and the oldest past MAX_RECENT go. */
export function recordQuery(storage: StorageLike | null, org: string, project: string, run: RecentQuery) {
  if (!storage) return
  const kept = readRecent(storage, org, project).filter(r => r.query !== run.query)
  try {
    storage.setItem(keyOf(org, project), JSON.stringify([run, ...kept].slice(0, MAX_RECENT)))
  } catch {
    // Storage is full or blocked: this run is not remembered.
  }
}

/** The newest `limit` recent Queries of several Projects, each with its Project's slug. */
export function latestQueries(storage: StorageLike | null, org: string, projects: string[], limit = MAX_RECENT) {
  return projects
    .flatMap(project => readRecent(storage, org, project).map(r => ({ project, ...r })))
    .sort((a, b) => b.at - a.at)
    .slice(0, limit)
}

function isRecent(value: unknown): value is RecentQuery {
  const r = value as RecentQuery | null
  return typeof r === 'object' && r !== null && typeof r.query === 'string' && typeof r.search === 'string' && typeof r.at === 'number'
}
