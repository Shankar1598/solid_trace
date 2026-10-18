// A chart's axes and numbers: where ticks fall and how they read, times in
// the viewer's local time and values on round numbers, written to the
// precision their step needs.
import { addDays, addHours, format, startOfDay, startOfHour } from 'date-fns'

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

/**
 * Local times to a bin's precision, with the date once the range spans days.
 * Bins of a day or more start at UTC midnights, so away from UTC they start
 * at a local time that their label shows, lest a bar under a day's tick read
 * as the day before.
 */
export function timePattern(binSeconds: number, { from, to }: { from: Date; to: Date }): string {
  if (binSeconds >= 86_400) {
    const step = binSeconds * 1000
    const firstBin = Math.floor(from.getTime() / step) * step
    return startOfDay(firstBin).getTime() === firstBin ? 'MMM dd' : 'MMM dd HH:mm'
  }
  const time = binSeconds < 60 ? 'HH:mm:ss' : 'HH:mm'
  return format(from, 'yyyyMMdd') === format(to, 'yyyyMMdd') ? time : `MMM dd ${time}`
}

/**
 * At most max ticks in [from, to] (epoch ms), on round local times: the
 * smallest step that fits. Steps under an hour fall on whole steps of the
 * local clock, hours on local hours that the step divides (00:00, 06:00, …
 * whatever the zone's offset), and days on local midnights.
 */
/**
 * The ticks whose labels, centred on them, fit inside the chart: a label that
 * would run past either edge is left out rather than cut off. The plot spans
 * `plot` of a chart `width` wide; before it's measured every tick stays.
 */
export function ticksInside(ticks: number[], domain: [number, number], plot: [number, number], width: number, halfLabel: number): number[] {
  const [from, to] = domain
  if (width <= 0 || to <= from) return ticks
  const [left, right] = plot
  return ticks.filter(t => {
    const x = left + ((t - from) / (to - from)) * (right - left)
    return x - halfLabel >= 0 && x + halfLabel <= width
  })
}

export function timeTicks(from: number, to: number, max: number): number[] {
  if (to <= from) return []
  const step = STEPS.find(s => (to - from) / s <= max) ?? STEPS[STEPS.length - 1]
  const ticks: number[] = []
  if (step < HOUR) {
    const offset = -new Date(from).getTimezoneOffset() * MINUTE
    for (let t = Math.ceil((from + offset) / step) * step - offset; t <= to; t += step) ticks.push(t)
    return ticks
  }
  if (step < DAY) {
    const hours = step / HOUR
    for (let t = startOfHour(from); t.getTime() <= to; t = addHours(t, 1)) {
      if (t.getTime() >= from && t.getHours() % hours === 0) ticks.push(t.getTime())
    }
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
 * negative values, to just past the largest, in about `steps` round steps
 * (fewer suit a short chart).
 */
export function valueTicks(min: number, max: number, steps = 5): { domain: [number, number]; ticks: number[]; step: number } {
  const low = Math.min(0, Number.isFinite(min) ? min : 0)
  let high = Math.max(0, Number.isFinite(max) ? max : 0)
  if (high === low) high = low + 1
  const rough = (high - low) / steps
  const power = 10 ** Math.floor(Math.log10(rough))
  const step = Number(NICE.map(n => n * power).find(s => s >= rough)!.toPrecision(12))
  const first = Math.floor(low / step)
  const last = Math.ceil(high / step)
  const ticks = Array.from({ length: last - first + 1 }, (_, i) => Number(((first + i) * step).toPrecision(12)))
  return { domain: [ticks[0], ticks[ticks.length - 1]], ticks, step }
}

/**
 * How the ticks of a value axis `step` apart read: compact from 1,000 up, as
 * 1K, with every digit down to the step's last, so 1,250 reads 1.25K and
 * 0.05 reads 0.05, and no two ticks read the same. Counting significant
 * digits keeps that true whatever units the viewer's locale uses (lakh and
 * crore, or 万 and 億, as well as K and M); `locale` is for tests.
 */
export function tickLabel(step: number, locale?: string): (value: number) => string {
  const last = lastDigit(step)
  return value => {
    if (value === 0) return numberFormat({}, locale).format(0)
    const digits = Math.min(21, Math.max(1, leadingDigit(value) - last + 1))
    return numberFormat({ notation: 'compact', maximumSignificantDigits: digits }, locale).format(value)
  }
}

/** A label's character width at 10px in the mono font, and the room beside its text. */
export const CHAR_PX = 6
export const LABEL_PAD_PX = 12
/** The fewest characters of a category's name beside a lying bar, and the share of a wide chart its names may take. */
const NAME_CHARS = 20
const NAME_SHARE = 0.38

/** The characters of a category's name shown beside a lying bar in a chart `width` px wide: about 20 on a phone, more on a desktop. */
export function nameChars(width: number): number {
  return Math.max(NAME_CHARS, Math.floor((width * NAME_SHARE - LABEL_PAD_PX) / CHAR_PX))
}

/**
 * A name cut to max characters in the middle, keeping more of its start than
 * its end: names that share a start, such as Log messages and paths, mostly
 * differ at their end.
 */
export function shortName(name: string, max: number): string {
  const chars = [...name]
  if (chars.length <= max) return name
  const head = Math.ceil((max - 1) * 0.6)
  return `${chars.slice(0, head).join('')}…${chars.slice(chars.length - (max - 1 - head)).join('')}`
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

/** How a Dashboard chart reads its values: a fixed number of decimals and a unit after them. */
export interface ValueOptions {
  unit?: string
  decimals?: number
}

/** A value read with a chart's options: `decimals` fixed, else as `plain` reads it, then the unit (`%` close up). */
export function optionsText(value: number, options: ValueOptions, plain: (value: number) => string = valueText): string {
  const text = options.decimals === undefined
    ? plain(value)
    : numberFormat({ minimumFractionDigits: options.decimals, maximumFractionDigits: options.decimals }).format(value)
  return withUnit(text, options.unit)
}

/** A number's text with a chart's unit after it, `%` close up: 6K ms, 40%. Value axes label their ticks so too. */
export function withUnit(text: string, unit?: string): string {
  if (!unit) return text
  return unit === '%' ? `${text}%` : `${text} ${unit}`
}

/** The power of ten of a number's leading digit: 2 for 250, -2 for 0.025. */
function leadingDigit(n: number): number {
  return Number(Number(n.toPrecision(12)).toExponential().split('e')[1])
}

/** The power of ten of a number's last significant digit: 1 for 250, -3 for 0.025. */
function lastDigit(n: number): number {
  const [mantissa, exponent] = Number(n.toPrecision(12)).toExponential().split('e')
  return Number(exponent) - (mantissa.split('.')[1]?.length ?? 0)
}

const formats = new Map<string, Intl.NumberFormat>()
function numberFormat(options: Intl.NumberFormatOptions, locale?: string): Intl.NumberFormat {
  const key = `${locale ?? ''} ${JSON.stringify(options)}`
  let format = formats.get(key)
  if (!format) formats.set(key, (format = new Intl.NumberFormat(locale, options)))
  return format
}
