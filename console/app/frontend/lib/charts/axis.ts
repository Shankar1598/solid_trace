// A chart's axes and numbers: where ticks fall and how they read, times in
// the viewer's local time and values on round numbers, written to the
// precision their step needs.
import { addDays, format, startOfDay } from 'date-fns'

const SECOND = 1000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** Steps between ticks, smallest first; from a day up they fall on local midnights. */
const STEPS = [
  SECOND, 2 * SECOND, 5 * SECOND, 10 * SECOND, 15 * SECOND, 30 * SECOND,
  MINUTE, 2 * MINUTE, 5 * MINUTE, 10 * MINUTE, 15 * MINUTE, 30 * MINUTE,
  HOUR, 2 * HOUR, 3 * HOUR, 6 * HOUR, 12 * HOUR,
  DAY, 2 * DAY, 7 * DAY, 14 * DAY, 30 * DAY,
]

/** Local times to a bin's precision, with the date once the range spans days. */
export function timePattern(binSeconds: number, { from, to }: { from: Date; to: Date }): string {
  if (binSeconds >= 86_400) return 'MMM dd'
  const time = binSeconds < 60 ? 'HH:mm:ss' : 'HH:mm'
  return format(from, 'yyyyMMdd') === format(to, 'yyyyMMdd') ? time : `MMM dd ${time}`
}

/** At most max ticks in [from, to] (epoch ms), on round times: the smallest step that fits. */
export function timeTicks(from: number, to: number, max: number): number[] {
  if (to <= from) return []
  const step = STEPS.find(s => (to - from) / s <= max) ?? STEPS[STEPS.length - 1]
  const ticks: number[] = []
  if (step < DAY) {
    for (let t = Math.ceil(from / step) * step; t <= to; t += step) ticks.push(t)
    return ticks
  }
  const days = step / DAY
  let day = startOfDay(new Date(from))
  if (day.getTime() < from) day = addDays(day, 1)
  for (; day.getTime() <= to; day = addDays(day, days)) ticks.push(day.getTime())
  return ticks
}

/** Steps between value ticks, times a power of ten. */
const NICE = [1, 2, 2.5, 5, 10]

/**
 * The value axis for values from min to max: from zero, or below it for
 * negative values, to just past the largest, in about five round steps.
 */
export function valueTicks(min: number, max: number): { domain: [number, number]; ticks: number[]; step: number } {
  const low = Math.min(0, Number.isFinite(min) ? min : 0)
  let high = Math.max(0, Number.isFinite(max) ? max : 0)
  if (high === low) high = low + 1
  const rough = (high - low) / 5
  const power = 10 ** Math.floor(Math.log10(rough))
  const step = Number(NICE.map(n => n * power).find(s => s >= rough)!.toPrecision(12))
  const first = Math.floor(low / step)
  const last = Math.ceil(high / step)
  const ticks = Array.from({ length: last - first + 1 }, (_, i) => Number(((first + i) * step).toPrecision(12)))
  return { domain: [ticks[0], ticks[ticks.length - 1]], ticks, step }
}

/** Compact notation's units: thousands, millions, billions and trillions. */
const UNITS = [1, 1e3, 1e6, 1e9, 1e12]

/**
 * How the ticks of a value axis `step` apart read: compact from 1,000 up, as
 * 1K, with as many decimals as the step needs in the tick's unit, so 1,250
 * reads 1.25K and 0.05 reads 0.05, and no two ticks read the same.
 */
export function tickLabel(step: number): (value: number) => string {
  return value => {
    const abs = Math.abs(value)
    const unit = UNITS.filter(u => abs >= u).at(-1) ?? 1
    return numberFormat({ notation: 'compact', maximumFractionDigits: decimals(step / unit) }).format(value)
  }
}

/** A value as a tooltip reads it: to two decimals, or three significant digits under one. */
export function valueText(value: number): string {
  const abs = Math.abs(value)
  return numberFormat(abs > 0 && abs < 1 ? { maximumSignificantDigits: 3 } : { maximumFractionDigits: 2 }).format(value)
}

/** A single number as a stat tile reads it: compact from a million up, as 4.21M. */
export function statText(value: number): string {
  return Math.abs(value) >= 1e6
    ? numberFormat({ notation: 'compact', maximumSignificantDigits: 3 }).format(value)
    : valueText(value)
}

/** The decimals a number needs: 2 for 0.25, none for 250. */
function decimals(n: number): number {
  const [mantissa, exponent] = Number(n.toPrecision(12)).toExponential().split('e')
  return Math.min(20, Math.max(0, (mantissa.split('.')[1]?.length ?? 0) - Number(exponent)))
}

const formats = new Map<string, Intl.NumberFormat>()
function numberFormat(options: Intl.NumberFormatOptions): Intl.NumberFormat {
  const key = JSON.stringify(options)
  let format = formats.get(key)
  if (!format) formats.set(key, (format = new Intl.NumberFormat(undefined, options)))
  return format
}
