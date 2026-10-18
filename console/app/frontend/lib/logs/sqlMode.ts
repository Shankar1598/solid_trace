// SQL mode: SQL text seeded with the readable SQL of the current Query. It is
// one-way: the browser never turns SQL back into a Query, so leaving SQL mode
// drops the SQL and returns to the Query it was opened from, asking first
// when the SQL is no longer the SQL it opened with.
import { draftIn, searchOf, type Explorer, type Mode } from './explorerState'

/**
 * Into SQL mode with `sql`, the readable SQL of the Query, as both what ran
 * and the draft, so the URL holds it. Nothing runs; `replace` is the search
 * to put in place of the current history entry.
 */
export function enterSql(explorer: Explorer, sql: string): { explorer: Explorer; replace: string } {
  const current = { ...explorer.current, mode: 'sql' as const, sql }
  return { explorer: { current, draft: { ...explorer.draft, mode: 'sql', sql } }, replace: searchOf(current) }
}

/**
 * SQL mode → `mode`. `seed` is the SQL it opened with, or null when it opened
 * from a link (SQL it didn't write, so any SQL counts as edits). Unedited SQL
 * switches silently; edited SQL, typed or run, asks first.
 */
export type LeaveSql =
  | { kind: 'switch'; explorer: Explorer; replace: string }
  | { kind: 'ask'; mode: Mode }

export function leaveSql(explorer: Explorer, seed: string | null, mode: Mode): LeaveSql {
  const edited = [explorer.current.sql, explorer.draft.sql].some(sql => sql !== '' && sql !== seed)
  if (edited) return { kind: 'ask', mode }
  return { kind: 'switch', ...discardSql(explorer, mode) }
}

/**
 * Drops the SQL, back to the Query in `mode` (the Builder shows the Query that
 * ran, see draftIn); `replace` is the current history entry's new search.
 */
export function discardSql(explorer: Explorer, mode: Mode): { explorer: Explorer; replace: string } {
  const current = { ...explorer.current, mode, sql: '' }
  return { explorer: { current, draft: draftIn({ ...explorer.draft, sql: '' }, current) }, replace: searchOf(current) }
}
