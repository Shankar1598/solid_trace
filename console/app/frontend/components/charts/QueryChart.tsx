import { useMemo, useState } from 'react'
import { format } from 'date-fns'
import { Area, AreaChart, Bar, BarChart, CartesianGrid, Line, LineChart, ReferenceArea, XAxis, YAxis } from 'recharts'
import type { MouseHandlerDataParam } from 'recharts'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { MAX_SERIES, chartOf, formFor, type ChartForm, type ChartModel, type ChartPoint, type Series } from '@/lib/charts/series'
import { statText, tickLabel, timePattern, timeTicks, valueText, valueTicks } from '@/lib/charts/axis'
import type { AbsoluteRange } from '@/lib/logs/explorerState'
import { zoomRange } from '@/lib/logs/histogram'
import type { LogTable } from '@/lib/logs/table'
import { cn } from '@/lib/utils'

interface QueryChartProps {
  table: LogTable
  /** How to draw it; the form its shape suggests when absent, or when it can't be drawn so. */
  form?: ChartForm
  /** Takes the range dragged across a time chart; without it, dragging does nothing. */
  onZoom?: (range: AbsoluteRange) => void
  /** Controls at the end of the legend's line, such as the explorer's chart switch. */
  actions?: React.ReactNode
  className?: string
}

/** The most time ticks a chart labels; fewer show when they would crowd. */
const TIME_TICKS = 6

/**
 * A Query's table drawn as a time series (line, area or bars), bars over
 * categories, or a single number. The legend shows and hides series; dragging
 * across a time chart zooms to the bins dragged over. Used wherever a Query's
 * table is charted, so it takes the table and options, never a page's state.
 */
export function QueryChart({ table, form, onZoom, actions, className }: QueryChartProps) {
  const model = useMemo(() => chartOf(table), [table])
  const drawn = formFor(model, form ?? 'auto')
  if (model.kind === 'none' || drawn === null) return null
  if (model.kind === 'stat') {
    return (
      <div className={cn('flex items-start justify-between gap-3 px-3 py-2', className)}>
        <StatTile name={model.name} value={model.value} />
        {actions}
      </div>
    )
  }
  return <SeriesChart key={tableKey(table)} model={model} form={drawn} onZoom={onZoom} actions={actions} className={className} />
}

/** One number: its column's name over it, large. */
function StatTile({ name, value }: { name: string; value: number | null }) {
  return (
    <div className="min-w-0" data-chart-stat>
      <div className="truncate text-[11px] text-muted-foreground">{name}</div>
      <div className="truncate text-3xl font-semibold leading-tight text-foreground" title={value === null ? undefined : String(value)}>
        {value === null ? '—' : statText(value)}
      </div>
    </div>
  )
}

type SeriesChartModel = Extract<ChartModel, { kind: 'time' | 'bars' }>

function SeriesChart({ model, form, onZoom, actions, className }: {
  model: SeriesChartModel; form: ChartForm; onZoom?: QueryChartProps['onZoom']; actions?: React.ReactNode; className?: string
}) {
  const [hidden, setHidden] = useState<Set<string>>(() => new Set())
  const [drag, setDrag] = useState<{ start: number; end: number } | null>(null)
  const config = useMemo<ChartConfig>(
    () => Object.fromEntries(model.series.map(s => [s.key, { label: s.name, color: s.color }])),
    [model],
  )
  const toggle = (key: string) => setHidden(h => {
    const next = new Set(h)
    if (!next.delete(key)) next.add(key)
    return next
  })

  const time = model.kind === 'time' ? model : null
  // Bars cover their bin, so they sit on its middle; lines and areas mark its start.
  const binMs = time ? time.binSeconds * 1000 : 0
  const data = useMemo<ChartPoint[]>(
    () => (time && form === 'bars' ? model.points.map(p => ({ ...p, x: (p.x as number) + binMs / 2 })) : model.points),
    [model, form],
  )
  const domain: [number, number] | undefined = time && model.points.length > 0
    ? [Math.min(model.points[0].x as number, time.from.getTime()), Math.max((model.points.at(-1)!.x as number) + (form === 'bars' ? binMs : 0), time.to.getTime())]
    : undefined
  const pattern = time ? timePattern(time.binSeconds, time) : ''
  const label = (x: unknown) => (time ? format(new Date(Number(x) - (form === 'bars' ? binMs / 2 : 0)), pattern) : String(x))

  const index = (state: MouseHandlerDataParam) => {
    const i = Number(state.activeTooltipIndex)
    return Number.isInteger(i) && i >= 0 && i < model.points.length ? i : null
  }
  const zooms = time !== null && onZoom !== undefined
  const zoomEvents = zooms ? {
    onMouseDown: (state: MouseHandlerDataParam) => {
      const i = index(state)
      if (i !== null) setDrag({ start: i, end: i })
    },
    onMouseMove: (state: MouseHandlerDataParam) => {
      const i = index(state)
      if (drag && i !== null && i !== drag.end) setDrag({ ...drag, end: i })
    },
    onMouseUp: () => {
      if (!drag) return
      setDrag(null)
      // A click is no zoom: only a drag across bins narrows the range.
      if (drag.start === drag.end) return
      const buckets = model.points.map(p => ({ time: new Date(p.x as number).toISOString() }))
      onZoom(zoomRange({ bucket_seconds: time.binSeconds, buckets }, time, drag.start, drag.end))
    },
    onMouseLeave: () => setDrag(null),
  } : {}
  const dragArea = drag && time && drag.start !== drag.end ? (() => {
    const low = model.points[Math.min(drag.start, drag.end)].x as number
    const high = (model.points[Math.max(drag.start, drag.end)].x as number) + binMs
    return <ReferenceArea x1={low} x2={Math.min(high, domain![1])} fill="var(--primary)" fillOpacity={0.12} stroke="none" ifOverflow="hidden" />
  })() : null

  const visible = model.series.filter(s => !hidden.has(s.key))
  const top = visible.at(-1)?.key
  const stack = model.stacked ? 'stack' : undefined
  const values = valueTicks(...extent(model.points, visible, stack !== undefined && form !== 'line'))
  // A value with gaps either side draws no line, so it gets a dot.
  const dotFor = (s: Series) => (props: { cx?: number; cy?: number; index?: number }) => {
    const i = props.index ?? -1
    const lone = model.points[i]?.[s.key] != null && model.points[i - 1]?.[s.key] == null && model.points[i + 1]?.[s.key] == null
    return lone && props.cx !== undefined && props.cy !== undefined
      ? <circle key={`${s.key}-${i}`} cx={props.cx} cy={props.cy} r={4} fill={`var(--color-${s.key})`} stroke="var(--card)" strokeWidth={2} />
      : <g key={`${s.key}-${i}`} />
  }
  const chart = {
    data,
    margin: { top: 6, right: 12, bottom: 0, left: 0 },
    ...zoomEvents,
  }
  const axes = (
    <>
      <CartesianGrid vertical={false} stroke="var(--border)" />
      {time ? (
        <XAxis
          dataKey="x"
          type="number"
          scale="time"
          domain={domain}
          ticks={domain ? timeTicks(domain[0], domain[1], TIME_TICKS) : undefined}
          tickFormatter={x => format(new Date(x), pattern)}
          tickLine={false}
          axisLine={{ stroke: 'var(--border)' }}
          minTickGap={24}
          tick={{ fontSize: 10 }}
        />
      ) : (
        <XAxis
          dataKey="x"
          type="category"
          tickLine={false}
          axisLine={{ stroke: 'var(--border)' }}
          interval="preserveStartEnd"
          minTickGap={12}
          tick={{ fontSize: 10 }}
          tickFormatter={(x: string) => (x.length > 14 ? `${x.slice(0, 13)}…` : x)}
        />
      )}
      <YAxis
        width="auto"
        domain={values.domain}
        ticks={values.ticks}
        tickLine={false}
        axisLine={false}
        tick={{ fontSize: 10 }}
        tickFormatter={tickLabel(values.step)}
      />
      <ChartTooltip
        isAnimationActive={false}
        active={drag ? false : undefined}
        content={<ChartTooltipContent indicator="line" valueFormatter={valueText} labelFormatter={(_, payload) => label(payload?.[0]?.payload?.x)} />}
      />
      {dragArea}
    </>
  )

  return (
    <div className={cn('flex flex-col', className)}>
      <div className="flex min-h-7 flex-wrap items-center gap-x-3 gap-y-1 px-3 pt-1.5" data-chart-legend>
        <Legend model={model} form={form} hidden={hidden} onToggle={toggle} />
        {actions && <div className="ml-auto">{actions}</div>}
      </div>
      <ChartContainer
        config={config}
        className={cn('aspect-auto h-44 w-full px-1 sm:h-56', zooms && 'cursor-crosshair select-none')}
        role="figure"
        aria-label={chartLabel(model, form)}
      >
        {form === 'bars' ? (
          <BarChart {...chart} barCategoryGap={time ? 1 : '20%'}>
            {axes}
            {model.series.map(s => (
              <Bar
                key={s.key}
                dataKey={s.key}
                name={s.name}
                hide={hidden.has(s.key)}
                stackId={stack}
                fill={`var(--color-${s.key})`}
                maxBarSize={24}
                radius={!stack || s.key === top ? [3, 3, 0, 0] : 0}
                stroke={stack ? 'var(--card)' : undefined}
                strokeWidth={stack ? 1 : 0}
                isAnimationActive={false}
              />
            ))}
          </BarChart>
        ) : form === 'area' ? (
          <AreaChart {...chart}>
            {axes}
            {model.series.map(s => (
              <Area
                key={s.key}
                dataKey={s.key}
                name={s.name}
                hide={hidden.has(s.key)}
                stackId={stack}
                type="linear"
                stroke={`var(--color-${s.key})`}
                strokeWidth={2}
                fill={`var(--color-${s.key})`}
                fillOpacity={stack ? 0.25 : 0.1}
                dot={dotFor(s)}
                activeDot={{ r: 4, stroke: 'var(--card)', strokeWidth: 2 }}
                isAnimationActive={false}
              />
            ))}
          </AreaChart>
        ) : (
          <LineChart {...chart}>
            {axes}
            {model.series.map(s => (
              <Line
                key={s.key}
                dataKey={s.key}
                name={s.name}
                hide={hidden.has(s.key)}
                type="linear"
                stroke={`var(--color-${s.key})`}
                strokeWidth={2}
                dot={dotFor(s)}
                activeDot={{ r: 4, stroke: 'var(--card)', strokeWidth: 2 }}
                isAnimationActive={false}
              />
            ))}
          </LineChart>
        )}
      </ChartContainer>
    </div>
  )
}

/**
 * The series' names: buttons that show and hide each when there are several,
 * else the one series' name, as a chart's title. Each wears a key of its mark,
 * a stroke for lines and a block for bars and areas; the text stays in ink.
 */
function Legend({ model, form, hidden, onToggle }: {
  model: SeriesChartModel; form: ChartForm; hidden: Set<string>; onToggle: (key: string) => void
}) {
  const notes = [
    model.omitted > 0 && {
      text: `${model.omitted.toLocaleString()} more not shown`,
      why: `Only the ${MAX_SERIES} series with the largest totals are drawn.`,
    },
    model.kind === 'bars' && model.omittedCategories > 0 && {
      text: `the top ${model.points.length} of ${(model.points.length + model.omittedCategories).toLocaleString()} ${model.category} values`,
      why: `Only the ${model.points.length} bars with the largest totals are drawn.`,
    },
  ].filter(note => note !== false).map(({ text, why }) => (
    <span key={text} className="text-[11px] text-muted-foreground" title={why}>{text}</span>
  ))

  if (model.series.length === 1) {
    return (
      <>
        <span className="truncate text-[11px] text-muted-foreground">{model.series[0].name}</span>
        {notes}
      </>
    )
  }
  return (
    <>
      {model.series.map(s => {
        const shown = !hidden.has(s.key)
        return (
          <button
            key={s.key}
            type="button"
            aria-pressed={shown}
            title={`${shown ? 'Hide' : 'Show'} ${s.name}`}
            onClick={() => onToggle(s.key)}
            className={cn(
              'flex max-w-56 items-center gap-1.5 rounded-sm px-1 text-[11px] hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring',
              shown ? 'text-foreground' : 'text-muted-foreground line-through',
            )}
          >
            <span
              className={cn('shrink-0', form === 'line' ? 'h-0.5 w-3 rounded-full' : 'h-2 w-2 rounded-[2px]', !shown && 'opacity-40')}
              style={{ background: s.color }}
            />
            <span className="truncate">{s.name}</span>
          </button>
        )
      })}
      {notes}
    </>
  )
}

/** The lowest and highest values drawn: of the shown series, or of their sums at each point when they stack. */
function extent(points: ChartPoint[], series: Series[], stacked: boolean): [number, number] {
  let low = Infinity
  let high = -Infinity
  for (const point of points) {
    let up = 0
    let down = 0
    for (const s of series) {
      const v = point[s.key]
      if (typeof v !== 'number') continue
      if (!stacked) {
        low = Math.min(low, v)
        high = Math.max(high, v)
      } else if (v >= 0) up += v
      else down += v
    }
    if (stacked) {
      low = Math.min(low, down)
      high = Math.max(high, up)
    }
  }
  return [low, high]
}

/** What the chart shows, for a screen reader: its series and what they are drawn over. */
function chartLabel(model: SeriesChartModel, form: ChartForm): string {
  const names = model.series.map((s: Series) => s.name).join(', ')
  const over = model.kind === 'time' ? 'over time' : `by ${model.category}`
  return `${form === 'line' ? 'Line' : form === 'area' ? 'Area' : 'Bar'} chart of ${names} ${over}`
}

/** Each table's number, so a new table, a new run's, starts with every series shown. */
const tableIds = new WeakMap<LogTable, number>()
let lastTableId = 0
function tableKey(table: LogTable): number {
  let id = tableIds.get(table)
  if (id === undefined) tableIds.set(table, (id = ++lastTableId))
  return id
}
