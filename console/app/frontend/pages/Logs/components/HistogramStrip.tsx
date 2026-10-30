import { useRef, useState } from 'react'
import { format } from 'date-fns'
import { Skeleton } from '@/components/ui/skeleton'
import { timePattern } from '@/lib/charts/axis'
import type { AbsoluteRange } from '@/lib/logs/explorerState'
import { elapsedText } from '@/lib/logs/runLine'
import {
  HISTOGRAM_LEVELS,
  barAt,
  errorRate,
  zoomRange,
  type Histogram,
  type HistogramBucket,
  type HistogramLevel,
} from '@/lib/logs/histogram'
import { cn } from '@/lib/utils'
import { LEVEL_TEXT } from './LogStream'

/** The strip's state for the current run. */
export type HistogramView =
  | { status: 'running' }
  | { status: 'cancelled' }
  | { status: 'error'; message: string }
  /** EventStore didn't run the Query: the histogram follows its run. */
  | { status: 'refused' }
  | { status: 'done'; histogram: Histogram; from: Date; to: Date }

const STRIP_HEIGHT = 48
/** The heights of the placeholder's bars while Logs are counted, in percent: a quiet day with two busy hours. */
const SKELETON_BARS = Array.from({ length: 60 }, (_, i) => 30 + 20 * Math.sin(i / 4) ** 2 + (i % 17 === 5 ? 40 : 0))
const TIME_LABELS = 5

/** Bottom to top in a bar, so errors sit on the axis where they're easiest to see. */
const STACK: HistogramLevel[] = ['fatal', 'error', 'warn', 'info', 'debug', 'trace', 'none']

/** The Levels' colours, the ones series of Levels wear in a chart (globals.css). */
const LEVEL_FILL: Record<HistogramLevel, string> = {
  trace: 'bg-(--chart-level-trace)',
  debug: 'bg-(--chart-level-debug)',
  info: 'bg-(--chart-level-info)',
  warn: 'bg-(--chart-level-warn)',
  error: 'bg-(--chart-level-error)',
  fatal: 'bg-(--chart-level-fatal)',
  none: 'bg-(--chart-level-none)',
}

const LEVEL_NAME: Record<HistogramLevel, string> = {
  trace: 'trace', debug: 'debug', info: 'info', warn: 'warn', error: 'error', fatal: 'fatal', none: 'no Level',
}

interface HistogramStripProps {
  view: HistogramView
  /** Runs the explorer over a narrower range. */
  onZoom: (range: AbsoluteRange) => void
  /** Filters on a Level, or null while no filter can be added. */
  onLevel: ((level: HistogramLevel) => void) | null
}

/** What the strip says while it has no counts. */
function notCounted(view: Exclude<HistogramView, { status: 'done' }>): string {
  switch (view.status) {
    case 'running': return 'Counting Logs…'
    case 'cancelled': return 'Not counted.'
    case 'refused': return "Not counted: the Query wasn't run."
    case 'error': return `Not counted: ${view.message}`
  }
}

/** The summary line and a slim histogram of the run's Logs, stacked by Level. */
export function HistogramStrip({ view, onZoom, onLevel }: HistogramStripProps) {
  return (
    <div className="border-b px-3 pt-1.5 pb-1" data-histogram-strip>
      {view.status === 'done' ? (
        <>
          <SummaryLine histogram={view.histogram} onLevel={onLevel} />
          <Strip histogram={view.histogram} range={view} onZoom={onZoom} />
        </>
      ) : (
        <>
          {/* At least the summary line's height, growing when a long message wraps on a phone. */}
          <div className="min-h-5 text-[11px] leading-5 text-muted-foreground">
            {notCounted(view)}
          </div>
          {view.status === 'running' && (
            <div className="flex items-end gap-px pt-1 pb-4" style={{ height: STRIP_HEIGHT + 16 }}>
              {SKELETON_BARS.map((height, i) => <Skeleton key={i} className="flex-1" style={{ height: `${height}%` }} />)}
            </div>
          )}
        </>
      )}
    </div>
  )
}

function SummaryLine({ histogram, onLevel }: { histogram: Histogram; onLevel: HistogramStripProps['onLevel'] }) {
  const rate = errorRate(histogram)
  return (
    // On a phone the line wraps between its parts, never inside one.
    <div data-summary-line className="flex min-h-5 flex-wrap items-center gap-x-3 whitespace-nowrap text-[11px] text-muted-foreground tabular-nums">
      <span>
        <span className="font-medium text-foreground">{histogram.count.toLocaleString()}</span> {histogram.count === 1 ? 'Log' : 'Logs'}
        {histogram.elapsed_ms !== undefined && <> · {elapsedText(histogram.elapsed_ms)}</>}
      </span>
      {histogram.count > 0 && (
        <>
          <span>error rate <span className="font-medium text-foreground">{rateText(rate)}</span></span>
          <span className="h-3 w-px bg-border" />
        </>
      )}
      {HISTOGRAM_LEVELS.filter(level => histogram.levels[level] > 0).map(level => {
        const label = (
          <>
            <span className={cn('inline-block h-2 w-2', LEVEL_FILL[level])} />
            <span className={LEVEL_TEXT[level]}>{LEVEL_NAME[level]}</span>
            <span className="text-foreground">{histogram.levels[level].toLocaleString()}</span>
          </>
        )
        return onLevel && level !== 'none' ? (
          <button
            key={level}
            className="flex items-center gap-1 px-1 hover:bg-muted"
            title={`Show only ${level} Logs`}
            onClick={() => onLevel(level)}
          >
            {label}
          </button>
        ) : (
          <span key={level} className="flex items-center gap-1 px-1">{label}</span>
        )
      })}
    </div>
  )
}

function rateText(rate: number | null): string {
  if (rate === null) return '–'
  if (rate > 0 && rate < 0.001) return '<0.1%'
  return `${(rate * 100).toFixed(1)}%`
}

/** The bars: drag across them, or click one, to zoom to their time. */
function Strip({ histogram, range, onZoom }: {
  histogram: Histogram; range: { from: Date; to: Date }; onZoom: HistogramStripProps['onZoom']
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [drag, setDrag] = useState<{ start: number; end: number } | null>(null)
  const { buckets } = histogram
  const max = Math.max(1, ...buckets.map(b => b.count))

  const barUnder = (clientX: number) => {
    const box = ref.current!.getBoundingClientRect()
    return barAt(clientX - box.left, box.width, buckets.length)
  }

  if (buckets.length === 0) return <div style={{ height: STRIP_HEIGHT + 16 }} />

  const [low, high] = drag ? [Math.min(drag.start, drag.end), Math.max(drag.start, drag.end)] : [-1, -1]
  const pattern = timePattern(histogram.bucket_seconds, range)

  return (
    <div className="select-none">
      <div
        ref={ref}
        className="flex cursor-crosshair items-end gap-px"
        style={{ height: STRIP_HEIGHT }}
        onPointerDown={e => {
          if (e.button !== 0) return
          e.currentTarget.setPointerCapture(e.pointerId)
          const bar = barUnder(e.clientX)
          setDrag({ start: bar, end: bar })
        }}
        onPointerMove={e => drag && setDrag({ ...drag, end: barUnder(e.clientX) })}
        onPointerUp={() => {
          if (!drag) return
          setDrag(null)
          onZoom(zoomRange(histogram, range, drag.start, drag.end))
        }}
        onPointerCancel={() => setDrag(null)}
      >
        {buckets.map((bucket, i) => (
          <Bar key={bucket.time} bucket={bucket} max={max} selected={i >= low && i <= high} pattern={pattern} />
        ))}
      </div>
      <div className="relative h-4 font-mono text-[10px] leading-4 text-muted-foreground">
        {timeLabels(buckets, range).map(({ at, time }, k, all) => (
          <span
            key={k}
            className={cn(
              'absolute whitespace-nowrap',
              k === 0 ? '' : k === all.length - 1 ? '-translate-x-full' : '-translate-x-1/2',
              // A phone has room for the ends and the middle only.
              all.length === TIME_LABELS && (k === 1 || k === 3) && 'max-sm:hidden',
            )}
            style={{ left: `${at * 100}%` }}
          >
            {format(time, pattern)}
          </span>
        ))}
      </div>
    </div>
  )
}

function Bar({ bucket, max, selected, pattern }: { bucket: HistogramBucket; max: number; selected: boolean; pattern: string }) {
  const counts = STACK.filter(level => bucket.levels[level] > 0).map(level => `${LEVEL_NAME[level]} ${bucket.levels[level].toLocaleString()}`)
  return (
    <div
      className={cn('flex h-full min-w-0 flex-1 flex-col-reverse hover:bg-muted', selected && 'bg-primary/15 hover:bg-primary/15')}
      title={`${format(new Date(bucket.time), pattern)}: ${bucket.count.toLocaleString()} ${bucket.count === 1 ? 'Log' : 'Logs'}${counts.length ? `\n${counts.join(', ')}` : ''}`}
    >
      {STACK.map(level => bucket.levels[level] > 0 && (
        <div key={level} className={LEVEL_FILL[level]} style={{ height: `${(bucket.levels[level] / max) * 100}%` }} />
      ))}
    </div>
  )
}

/**
 * The strip's time labels, `at` a fraction of its width: the range's start
 * and end at the edges, and the starts of a few bars between.
 */
function timeLabels(buckets: HistogramBucket[], { from, to }: { from: Date; to: Date }): { at: number; time: Date }[] {
  const between = Math.min(TIME_LABELS - 2, buckets.length - 1)
  const inner = Array.from({ length: Math.max(0, between) }, (_, k) => {
    const i = Math.round(((k + 1) * buckets.length) / (between + 1))
    return { at: i / buckets.length, time: new Date(buckets[i].time) }
  })
  return [{ at: 0, time: from }, ...inner, { at: 1, time: to }]
}
