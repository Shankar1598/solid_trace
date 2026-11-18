// Threshold lines: fixed values a time chart draws as horizontal lines
// across its plot, such as a Dashboard chart's limits or a Monitor's
// threshold. A threshold line is not a series: the chart's groups do not
// split it, it is not in the legend, and it takes none of the series.
import { optionsText, type ValueOptions } from './axis'

export type Severity = 'warn' | 'critical'

export interface Threshold {
  value: number
  /** What the line shows at its right end; the value with the chart's unit and decimals when absent. */
  label?: string
  /** The line's colour: muted when absent. */
  severity?: Severity
}

/** One threshold line as it is drawn: where, the text at its right end, and its colour. */
export interface ThresholdMark {
  value: number
  label: string
  color: string
}

/** The colour of each severity, a token both themes define. */
const COLORS: Record<Severity | 'none', string> = {
  none: 'var(--muted-foreground)',
  warn: 'var(--warning)',
  critical: 'var(--destructive)',
}

/** The lines to draw, one for each threshold, in the order given. */
export function thresholdMarks(thresholds: readonly Threshold[] | undefined, options: ValueOptions): ThresholdMark[] {
  return (thresholds ?? []).map(t => ({
    value: t.value,
    label: t.label || optionsText(t.value, options),
    color: COLORS[t.severity ?? 'none'],
  }))
}

/** A value axis's lowest and highest values, widened to reach each threshold, so a line past every value still shows. */
export function thresholdReach([low, high]: [number, number], thresholds: readonly Threshold[]): [number, number] {
  return thresholds.reduce<[number, number]>(([l, h], t) => [Math.min(l, t.value), Math.max(h, t.value)], [low, high])
}
