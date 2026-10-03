// The histogram strip and summary line over the stream: the Logs that pass
// a run's filters, counted per bucket and Level by EventStore. Buckets start
// on whole multiples of their size from the epoch, so the first and last may
// reach outside the run's range.
import type { AbsoluteRange } from './explorerState'

const MINUTE = 60
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** Bucket sizes in seconds, smallest first. */
const BUCKET_LADDER = [
  1, 2, 5, 10, 15, 30,
  MINUTE, 2 * MINUTE, 5 * MINUTE, 10 * MINUTE, 15 * MINUTE, 30 * MINUTE,
  HOUR, 2 * HOUR, 3 * HOUR, 6 * HOUR, 12 * HOUR,
  DAY, 2 * DAY, 7 * DAY,
]

/** The most bars the strip shows for a range, before the ladder runs out. */
const MAX_BARS = 100

/** The bucket size for a range: the smallest that gives at most 100 bars. */
export function bucketSeconds(from: Date, to: Date): number {
  const seconds = (to.getTime() - from.getTime()) / 1000
  return BUCKET_LADDER.find(size => seconds / size <= MAX_BARS) ?? BUCKET_LADDER[BUCKET_LADDER.length - 1]
}

/** The buckets' starts and size, all zooming needs. */
export interface Buckets {
  bucket_seconds: number
  buckets: { time: string }[]
}

/**
 * The range from bar `a` to bar `b` (a click is a = b), either way round,
 * cut to the run's range: zooming narrows and never widens.
 */
export function zoomRange({ bucket_seconds, buckets }: Buckets, range: { from: Date; to: Date }, a: number, b: number): AbsoluteRange {
  const first = new Date(buckets[Math.min(a, b)].time).getTime()
  const last = new Date(buckets[Math.max(a, b)].time).getTime() + bucket_seconds * 1000
  return {
    kind: 'absolute',
    from: new Date(Math.max(first, range.from.getTime())),
    to: new Date(Math.min(last, range.to.getTime())),
  }
}

/** The bar under a pointer `x` pixels into a strip `width` wide, or the nearest end bar. */
export function barAt(x: number, width: number, bars: number): number {
  return Math.min(bars - 1, Math.max(0, Math.floor((x / width) * bars)))
}
