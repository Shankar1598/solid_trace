// The run line: what a run found and how long EventStore took to answer,
// from the elapsed_ms at the top of its query, histogram and sql answers.

/** "1,415 Logs · 230 ms" or "12 rows · 1.4 s"; the count alone when the time isn't known. */
export function runLine(count: number, noun: 'Log' | 'row', elapsedMs: number | null): string {
  const counted = `${count.toLocaleString('en-US')} ${count === 1 ? noun : `${noun}s`}`
  return elapsedMs === null ? counted : `${counted} · ${elapsedText(elapsedMs)}`
}

/**
 * The run line of a time chart's totals: "5 groups over the range · 230 ms",
 * or "Totals over the range" when nothing but the time bin groups them.
 */
export function totalsLine(groups: number | null, elapsedMs: number | null): string {
  const counted = groups === null ? 'Totals over the range' : `${groups.toLocaleString('en-US')} ${groups === 1 ? 'group' : 'groups'} over the range`
  return elapsedMs === null ? counted : `${counted} · ${elapsedText(elapsedMs)}`
}

/** 230 ms under a second, then seconds: 1.4 s, and whole ones from 10 s. */
export function elapsedText(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`
  const seconds = ms / 1000
  return seconds < 10 ? `${seconds.toFixed(1)} s` : `${Math.round(seconds)} s`
}

/** An answer's elapsed_ms, or null when it has none. */
export function elapsedOf(body: unknown): number | null {
  const ms = (body as { elapsed_ms?: unknown } | null)?.elapsed_ms
  return typeof ms === 'number' && Number.isFinite(ms) ? ms : null
}
