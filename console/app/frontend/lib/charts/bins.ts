// A time chart's bins in words: how big each is, so nobody reads a 5-minute
// count as a per-minute one, and the span each covers.
import { format, isSameDay, startOfDay } from 'date-fns'

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

/**
 * A bin's span, start to end, in local time, such as 19:02–19:03: to the
 * minute, or to the second or millisecond when the bin isn't a whole number
 * of minutes or seconds. The date leads when the range spans days or bins
 * are days, and the end leaves it out when both ends fall on one day. A bin
 * of days that starts at local midnight reads as days alone.
 */
export function binSpan(start: number, binSeconds: number, { from, to }: { from: Date; to: Date }): string {
  const end = start + binSeconds * 1000
  const days = binSeconds >= 86_400
  const time = days && startOfDay(start).getTime() === start ? ''
    : binSeconds % 1 ? 'HH:mm:ss.SSS'
      : binSeconds % 60 ? 'HH:mm:ss'
        : 'HH:mm'
  const dated = days || !isSameDay(from, to)
  const pattern = dated ? ['MMM dd', time].filter(Boolean).join(' ') : time
  const endPattern = dated && isSameDay(start, end) ? time : pattern
  return `${format(start, pattern)}–${format(end, endPattern)}`
}
