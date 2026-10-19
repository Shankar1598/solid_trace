// A time chart's bins in words: how big each is, so nobody reads a 5-minute
// count as a per-minute one, and the span each covers.
import { format, isSameDay } from 'date-fns'

/** Units a bin's size reads in, largest first, in seconds. */
const UNITS: [string, number][] = [['d', 86_400], ['h', 3_600], ['m', 60], ['s', 1]]

/**
 * A bin's size, such as 30s, 5m or 12h: a whole number of the largest unit
 * that fits, so 90 seconds reads 90s. A bin under a second, or not a whole
 * number of them, reads in milliseconds.
 */
export function binSize(seconds: number): string {
  const unit = UNITS.find(([, size]) => seconds >= size && seconds % size === 0)
  return unit ? `${seconds / unit[1]}${unit[0]}` : `${Math.round(seconds * 1000)}ms`
}

/** The date part a time pattern leads with when the range spans days (see timePattern). */
const DATED = 'MMM dd '

/**
 * A bin's span, start to end, in local time to the axis' pattern, such as
 * 19:02–19:03. When both ends fall on one day the end leaves out its date.
 */
export function binSpan(start: number, binSeconds: number, pattern: string): string {
  const end = start + binSeconds * 1000
  const endPattern = pattern.startsWith(DATED) && isSameDay(start, end) ? pattern.slice(DATED.length) : pattern
  return `${format(start, pattern)}–${format(end, endPattern)}`
}
