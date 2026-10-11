// Time ranges as the explorer, Dashboards and Monitors pick them: a span back
// from now, such as `15m`, `6h` or `7d`, which stays relative in a link, or
// absolute times. This module reads what a member types (`2h`, `last 3
// hours`, `yesterday`), names ranges for buttons, and checks them against a
// Project's Retention. Dates in and out of it are local times; links hold
// UTC ISO times (lib/logs/explorerState.ts).
import { addDays, format, isSameDay, startOfDay } from 'date-fns'

/** A span back from now: a whole number of minutes, hours or days, such as `15m`, `1h` or `7d`. */
export type RelativeRange = { kind: 'relative'; span: string }
export type AbsoluteRange = { kind: 'absolute'; from: Date; to: Date }
export type TimeRange = RelativeRange | AbsoluteRange

const SPAN = /^(\d+)([mhd])$/
const UNIT_MS = { m: 60_000, h: 3_600_000, d: 86_400_000 } as const
const DAY_MS = UNIT_MS.d

/** The ranges the picker offers at one click, shortest first. */
export const QUICK_RANGES: { span: string; label: string }[] = [
  { span: '5m', label: '5 mins' },
  { span: '15m', label: '15 mins' },
  { span: '30m', label: '30 mins' },
  { span: '1h', label: '1 hr' },
  { span: '3h', label: '3 hrs' },
  { span: '6h', label: '6 hrs' },
  // As links have always written a day.
  { span: '24h', label: '1 day' },
  { span: '2d', label: '2 days' },
  { span: '7d', label: '7 days' },
  { span: '15d', label: '15 days' },
  { span: '30d', label: '30 days' },
  { span: '90d', label: '90 days' },
]

/** Whether `span` is a span a link can hold: a whole number above 0 of m, h or d. */
export function isSpan(span: string): boolean {
  return SPAN.test(span) && spanMs(span) > 0
}

/** A span's length in milliseconds; 0 for text that isn't one. */
export function spanMs(span: string): number {
  const match = SPAN.exec(span)
  if (!match) return 0
  return Number(match[1]) * UNIT_MS[match[2] as keyof typeof UNIT_MS]
}

/** The times a range covers at `now`, `[from, to)`. */
export function resolveRange(range: TimeRange, now: Date): { from: Date; to: Date } {
  if (range.kind === 'absolute') return { from: range.from, to: range.to }
  return { from: new Date(now.getTime() - spanMs(range.span)), to: now }
}

/** How long a range is, in milliseconds. */
export function rangeMs(range: TimeRange): number {
  return range.kind === 'relative' ? spanMs(range.span) : range.to.getTime() - range.from.getTime()
}

/** Whether two ranges are the same: spans of one length (`24h` and `1d`), or the same two times. */
export function sameRange(a: TimeRange, b: TimeRange): boolean {
  if (a.kind === 'relative' && b.kind === 'relative') return spanMs(a.span) === spanMs(b.span)
  if (a.kind === 'absolute' && b.kind === 'absolute') return a.from.getTime() === b.from.getTime() && a.to.getTime() === b.to.getTime()
  return false
}

/** Whether a range is longer than `days`, a Project's Retention; never without one. */
export function longerThan(range: TimeRange, days: number | undefined): boolean {
  return days !== undefined && rangeMs(range) > days * DAY_MS
}

// The spans the old range select named, kept as they read.
const NAMED_SPANS: Record<string, string> = {
  '15m': 'Last 15 minutes',
  '1h': 'Last hour',
  '6h': 'Last 6 hours',
  '24h': 'Last 24 hours',
  '7d': 'Last 7 days',
  '30d': 'Last 30 days',
}

/** A span as words: `6h` is "Last 6 hours", `1h` "Last hour". */
export function spanLabel(span: string): string {
  if (NAMED_SPANS[span]) return NAMED_SPANS[span]
  const match = SPAN.exec(span)
  if (!match) return span
  const unit = { m: 'minute', h: 'hour', d: 'day' }[match[2] as 'm' | 'h' | 'd']
  return match[1] === '1' ? `Last ${unit}` : `Last ${match[1]} ${unit}s`
}

/**
 * A range as a button shows it: "Last 6 hours", or local times such as
 * "Oct 9 10:00 – 12:30", the day written once when both times share it.
 * Seconds show only in a range under an hour whose times have them (a zoom
 * into a short range), and the year only when it isn't `now`'s.
 */
export function rangeLabel(range: TimeRange, now: Date): string {
  if (range.kind === 'relative') return spanLabel(range.span)
  const { from, to } = range
  const seconds = rangeMs(range) < UNIT_MS.h && (from.getSeconds() !== 0 || to.getSeconds() !== 0)
  const time = seconds ? 'HH:mm:ss' : 'HH:mm'
  const day = (d: Date) => (d.getFullYear() === now.getFullYear() ? 'MMM d' : 'MMM d, yyyy')
  const end = isSameDay(from, to) ? format(to, time) : format(to, `${day(to)} ${time}`)
  return `${format(from, `${day(from)} ${time}`)} – ${end}`
}

export type Parsed = { range: TimeRange } | { error: string }

const UNITS: Record<string, 'm' | 'h' | 'd' | 'w'> = {
  m: 'm', min: 'm', mins: 'm', minute: 'm', minutes: 'm',
  h: 'h', hr: 'h', hrs: 'h', hour: 'h', hours: 'h',
  d: 'd', day: 'd', days: 'd',
  w: 'w', wk: 'w', wks: 'w', week: 'w', weeks: 'w',
}
const TYPED = /^(?:(last|past)\s*)?(\d+)?\s*([a-z]+)$/

/**
 * What a member typed, as a range: `15m`, `2h`, `3d`, `1w`, `90 mins`,
 * `last 3 hours`, `last hour`, `today` (midnight until now) or `yesterday`.
 * Weeks become days, which links keep. Null for an empty box.
 */
export function parseRange(text: string, now: Date): Parsed | null {
  const typed = text.trim().toLowerCase().replace(/\s+/g, ' ')
  if (typed === '') return null
  if (typed === 'today') return { range: { kind: 'absolute', from: startOfDay(now), to: now } }
  if (typed === 'yesterday') {
    const today = startOfDay(now)
    return { range: { kind: 'absolute', from: startOfDay(addDays(today, -1)), to: today } }
  }
  const match = TYPED.exec(typed)
  const unit = match && UNITS[match[3]]
  // A number or "last" before the unit: a bare "h" or "hours" is no range.
  if (!match || !unit || (match[1] === undefined && match[2] === undefined)) {
    return { error: 'Try 15m, 2h, 3d, 1w, last 3 hours, today or yesterday.' }
  }
  const n = Number(match[2] ?? 1)
  if (n <= 0) return { error: 'A range needs at least a minute.' }
  const span = unit === 'w' ? `${n * 7}d` : `${n}${unit}`
  return { range: { kind: 'relative', span } }
}

/** A time as a local date-time input shows it, to the minute: `2026-10-09T07:05`. */
export function localInput(date: Date): string {
  return format(date, "yyyy-MM-dd'T'HH:mm")
}

/** Two local date-time inputs as an absolute range. */
export function absoluteFromInputs(start: string, end: string): Parsed {
  const from = start === '' ? null : new Date(start)
  const to = end === '' ? null : new Date(end)
  if (!from || !to || isNaN(from.getTime()) || isNaN(to.getTime())) return { error: 'Pick a start and an end.' }
  if (from >= to) return { error: 'The start must be before the end.' }
  return { range: { kind: 'absolute', from, to } }
}
