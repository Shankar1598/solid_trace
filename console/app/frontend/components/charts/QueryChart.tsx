import { useMemo, useState } from 'react'
import { format } from 'date-fns'
import { Area, AreaChart, Bar, BarChart, CartesianGrid, Line, LineChart, ReferenceArea, XAxis, YAxis } from 'recharts'
import type { MouseHandlerDataParam } from 'recharts'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import {
  chartOf, formFor, keepColors, legendNotes,
  type ChartForm, type ChartPoint, type Series, type SeriesChartModel,
} from '@/lib/charts/series'
import { statText, tickLabel, timePattern, timeTicks, valueText, valueTicks } from '@/lib/charts/axis'
import type { AbsoluteRange } from '@/lib/logs/explorerState'
import { zoomRange } from '@/lib/logs/histogram'
import type { LogTable } from '@/lib/logs/table'
import { cn } from '@/lib/utils'

/**
 * The palette slot each series id wore in the last chart drawn in one place,
 * kept by its owner across tables so a re-run repaints no series.
 */
export interface ColorSlots {
  current: ReadonlyMap<string, number>
}

interface QueryChartProps {
  table: LogTable
  /** How to draw it; the form its shape suggests when absent, or when it can't be drawn so. */
  form?: ChartForm
  /** Takes the range dragged across a time chart; without it, dragging does nothing. */
  onZoom?: (range: AbsoluteRange) => void
  /** Controls at the end of the legend's line, such as the explorer's chart switch. */
  actions?: React.ReactNode
  /** Keeps each series' colour from the last table drawn here; without it, colours follow rank. */
  colors?: ColorSlots
  className?: string
}

/** The most time ticks a chart labels; fewer show when they would crowd. */
const TIME_TICKS = 6
/** A value tick's character width at 10px in the mono font, and the room beside its text. */
const TICK_CHAR_PX = 6
const TICK_PAD_PX = 12
/** Bars over more categories than this, or with longer names, lie across with their names beside them. */
const UPRIGHT_BARS = 8
const UPRIGHT_NAME = 12
/** A lying bar's band, and the most characters of a category's name beside it. */
const BAND_PX = 20
const NAME_CHARS = 20

/**
 * A Query's table drawn as a time series (line, area or bars), bars over
 * categories, or a single number. Columns that measure different things get
 * a panel each, over one time axis. The legend shows and hides series;
 * dragging across a time chart zooms to the bins dragged over. Used wherever
 * a Query's table is charted, so it takes the table and options, never a
 * page's state.
 */
export function QueryChart({ table, form, onZoom, actions, colors, className }: QueryChartProps) {
  const model = useMemo(() => {
    const made = chartOf(table)
    if (!colors) return made
    const kept = keepColors(made, colors.current)
    colors.current = kept.slots
    return kept.model
  }, [table, colors])
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

/** The bins a drag has covered, by their points' indexes. */
type Drag = { start: number; end: number }

function SeriesChart({ model, form, onZoom, actions, className }: {
  model: SeriesChartModel; form: ChartForm; onZoom?: QueryChartProps['onZoom']; actions?: React.ReactNode; className?: string
}) {
  const [hidden, setHidden] = useState<Set<string>>(() => new Set())
  const [drag, setDrag] = useState<Drag | null>(null)
  const config = useMemo<ChartConfig>(
    () => Object.fromEntries(model.series.map(s => [s.key, { label: s.name, color: s.color }])),
    [model],
  )
  const toggle = (id: string) => setHidden(h => {
    const next = new Set(h)
    if (!next.delete(id)) next.add(id)
    return next
  })

  const time = model.kind === 'time' ? model : null
  // Bars cover their bin, so they sit on its middle; lines and areas mark its start. Each point keeps its bin's start.
  const binMs = time ? time.binSeconds * 1000 : 0
  const data = useMemo<ChartPoint[]>(
    () => (time && form === 'bars' ? model.points.map(p => ({ ...p, x: (p.x as number) + binMs / 2, start: p.x })) : model.points),
    [model, form],
  )
  // Whole bins, so a bin the range covers in part keeps its width; only times in the range are labelled.
  const domain: [number, number] | undefined = time && model.points.length > 0
    ? [Math.min(model.points[0].x as number, time.from.getTime()), Math.max((model.points.at(-1)!.x as number) + (form === 'bars' ? binMs : 0), time.to.getTime())]
    : undefined
  const pattern = time ? timePattern(time.binSeconds, time) : ''

  // Each panel's value axis, over the series shown; one width for all, so their plots line up.
  // Many categories, or long names, read best as bars lying across, names beside them, the chart as tall as they need.
  const names = model.kind === 'bars' ? model.points.map(p => String(p.x)) : []
  const across = names.length > UPRIGHT_BARS || names.some(n => n.length > UPRIGHT_NAME)
  const nameWidth = Math.min(NAME_CHARS, Math.max(0, ...names.map(n => n.length))) * TICK_CHAR_PX + TICK_PAD_PX

  const several = model.panels.length > 1
  const panels = model.panels.map(panel => {
    const series = model.series.filter(s => panel.keys.includes(s.key))
    const visible = series.filter(s => !hidden.has(s.id))
    const stacked = panel.stacked && form !== 'line'
    const values = valueTicks(...extent(model.points, visible, stacked), several ? 3 : 5)
    return { panel, series, visible, stacked, values, label: tickLabel(values.step) }
  })
  const valueWidth = Math.max(...panels.map(({ values, label }) => Math.max(...values.ticks.map(t => label(t).length)))) * TICK_CHAR_PX + TICK_PAD_PX

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
  // A bin the range covers in part counts part of a bin, so it is faded, and with it the line into it.
  const partialAreas = time && domain ? time.partial.map(x => {
    const first = x < time.from.getTime()
    const [x1, x2] = form === 'bars' || first ? [x, x + binMs] : [x - binMs, x + binMs]
    return (
      <ReferenceArea
        key={x}
        x1={Math.max(x1, domain[0])}
        x2={Math.min(x2, domain[1])}
        fill="var(--card)"
        fillOpacity={0.6}
        stroke="none"
        ifOverflow="hidden"
        zIndex={450}
      />
    )
  }) : null

  const height = !several ? 'h-44 sm:h-56' : panels.length === 2 ? 'h-32 sm:h-40' : 'h-28 sm:h-32'

  return (
    <div className={cn('flex flex-col', className)}>
      <div className="flex min-h-7 flex-wrap items-center gap-x-3 gap-y-1 px-3 pt-1.5" data-chart-legend>
        <Legend model={model} form={form} hidden={hidden} onToggle={toggle} />
        {actions && <div className="ml-auto">{actions}</div>}
      </div>
      <div role="figure" aria-label={chartLabel(model, form)}>
        {panels.map(({ panel, series, stacked, values, label }, i) => (
          <div key={panel.keys[0]}>
            {several && <div className="truncate px-3 pt-1 text-[10px] text-muted-foreground" data-chart-panel>{panel.columns.join(', ')}</div>}
            <ChartContainer
              config={config}
              className={cn('aspect-auto w-full px-1', !across && height, zooms && 'cursor-crosshair select-none')}
              style={across ? { height: names.length * BAND_PX + 32 } : undefined}
            >
              <PanelChart
                form={form}
                data={data}
                series={series}
                hidden={hidden}
                stacked={stacked}
                model={model}
                across={across}
                axisBelow={across || i === panels.length - 1}
                xAxis={across ? (
                  <XAxis
                    type="number"
                    domain={values.domain}
                    ticks={values.ticks}
                    interval={0}
                    tickFormatter={label}
                    tickLine={false}
                    axisLine={{ stroke: 'var(--border)' }}
                    tick={{ fontSize: 10 }}
                  />
                ) : time ? (
                  <XAxis
                    dataKey="x"
                    type="number"
                    scale="time"
                    domain={domain}
                    ticks={timeTicks(time.from.getTime(), time.to.getTime(), TIME_TICKS)}
                    tickFormatter={x => format(new Date(x), pattern)}
                    tickLine={false}
                    axisLine={{ stroke: 'var(--border)' }}
                    minTickGap={24}
                    tick={{ fontSize: 10 }}
                    hide={i < panels.length - 1}
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
                    hide={i < panels.length - 1}
                  />
                )}
                yAxis={across ? (
                  <YAxis
                    type="category"
                    dataKey="x"
                    width={nameWidth}
                    interval={0}
                    tickLine={false}
                    axisLine={false}
                    tick={<NameTick />}
                  />
                ) : (
                  <YAxis
                    width={valueWidth}
                    domain={values.domain}
                    ticks={values.ticks}
                    interval={0}
                    tickLine={false}
                    axisLine={false}
                    tick={{ fontSize: 10 }}
                    tickFormatter={label}
                  />
                )}
                tooltip={(
                  <ChartTooltip
                    isAnimationActive={false}
                    active={drag ? false : undefined}
                    content={<ChartTooltipContent indicator="line" valueFormatter={valueText} labelFormatter={(_, payload) => pointLabel(payload?.[0]?.payload?.start ?? payload?.[0]?.payload?.x)} />}
                  />
                )}
                overlay={<>{partialAreas}{dragArea}</>}
                events={zoomEvents}
              />
            </ChartContainer>
          </div>
        ))}
      </div>
    </div>
  )

  /** A point's bin or category, for the tooltip: a bin's start in local time. */
  function pointLabel(x: unknown): string {
    if (!time) return String(x)
    const start = Number(x)
    return `${format(new Date(start), pattern)}${time.partial.includes(start) ? ' · partial bin' : ''}`
  }
}

/**
 * A category's name beside its bar, on one line: cut to NAME_CHARS with its
 * full name as a title. recharts would wrap it to the axis' width instead.
 */
function NameTick({ x, y, payload }: { x?: number; y?: number; payload?: { value: unknown } }) {
  const name = String(payload?.value ?? '')
  return (
    <text x={x} y={y} dx={-4} dy={3} textAnchor="end" fontSize={10} className="fill-muted-foreground">
      <title>{name}</title>
      {name.length > NAME_CHARS ? `${name.slice(0, NAME_CHARS - 1)}…` : name}
    </text>
  )
}

/** One panel's chart: its series in the form asked for, over the axes given. */
function PanelChart({ form, data, series, hidden, stacked, model, across, axisBelow, xAxis, yAxis, tooltip, overlay, events }: {
  form: ChartForm
  data: ChartPoint[]
  series: Series[]
  hidden: Set<string>
  stacked: boolean
  model: SeriesChartModel
  /** Bars lying across, categories down the side. */
  across: boolean
  /** Whether the x axis shows below; a panel above another hides it, and keeps room for its lowest value's label. */
  axisBelow: boolean
  xAxis: React.ReactNode
  yAxis: React.ReactNode
  tooltip: React.ReactNode
  overlay: React.ReactNode
  events: object
}) {
  const stack = stacked ? 'stack' : undefined
  const top = series.filter(s => !hidden.has(s.id)).at(-1)?.key
  // A value with gaps either side draws no line, so it gets a dot.
  const dotFor = (s: Series) => (props: { cx?: number; cy?: number; index?: number }) => {
    const i = props.index ?? -1
    const lone = model.points[i]?.[s.key] != null && model.points[i - 1]?.[s.key] == null && model.points[i + 1]?.[s.key] == null
    return lone && props.cx !== undefined && props.cy !== undefined
      ? <circle key={`${s.key}-${i}`} cx={props.cx} cy={props.cy} r={4} fill={`var(--color-${s.key})`} stroke="var(--card)" strokeWidth={2} />
      : <g key={`${s.key}-${i}`} />
  }
  const chart = { data, margin: { top: 6, right: 12, bottom: axisBelow ? 0 : 10, left: 0 }, ...events }
  const axes = (
    <>
      <CartesianGrid vertical={across} horizontal={!across} stroke="var(--border)" />
      {xAxis}
      {yAxis}
      {tooltip}
      {overlay}
    </>
  )

  if (form === 'bars') {
    return (
      <BarChart {...chart} layout={across ? 'vertical' : 'horizontal'} barCategoryGap={model.kind === 'time' ? 1 : '20%'}>
        {axes}
        {series.map(s => (
          <Bar
            key={s.key}
            dataKey={s.key}
            name={s.name}
            hide={hidden.has(s.id)}
            stackId={stack}
            fill={`var(--color-${s.key})`}
            maxBarSize={24}
            radius={!stack || s.key === top ? (across ? [0, 3, 3, 0] : [3, 3, 0, 0]) : 0}
            stroke={stack ? 'var(--card)' : undefined}
            strokeWidth={stack ? 1 : 0}
            isAnimationActive={false}
          />
        ))}
      </BarChart>
    )
  }
  if (form === 'area') {
    return (
      <AreaChart {...chart}>
        {axes}
        {series.map(s => (
          <Area
            key={s.key}
            dataKey={s.key}
            name={s.name}
            hide={hidden.has(s.id)}
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
    )
  }
  return (
    <LineChart {...chart}>
      {axes}
      {series.map(s => (
        <Line
          key={s.key}
          dataKey={s.key}
          name={s.name}
          hide={hidden.has(s.id)}
          type="linear"
          stroke={`var(--color-${s.key})`}
          strokeWidth={2}
          dot={dotFor(s)}
          activeDot={{ r: 4, stroke: 'var(--card)', strokeWidth: 2 }}
          isAnimationActive={false}
        />
      ))}
    </LineChart>
  )
}

/**
 * The series' names: buttons that show and hide each when there are several,
 * else the one series' name, as a chart's title. Each wears a key of its mark,
 * a stroke for lines and a block for bars and areas; the text stays in ink.
 */
function Legend({ model, form, hidden, onToggle }: {
  model: SeriesChartModel; form: ChartForm; hidden: Set<string>; onToggle: (id: string) => void
}) {
  const notes = legendNotes(model).map(({ text, why }) => (
    <span key={text} className="text-[11px] text-muted-foreground" title={why}>{text}</span>
  ))

  if (model.legend.length === 1) {
    return (
      <>
        {model.panels.length === 1 && <span className="truncate text-[11px] text-muted-foreground">{model.legend[0].name}</span>}
        {notes}
      </>
    )
  }
  return (
    <>
      {model.legend.map(entry => {
        const shown = !hidden.has(entry.id)
        return (
          <button
            key={entry.id}
            type="button"
            aria-pressed={shown}
            data-chart-series
            title={`${shown ? 'Hide' : 'Show'} ${entry.name}`}
            onClick={() => onToggle(entry.id)}
            className={cn(
              // 24px tall on a phone, a target a finger can hit; as dense as the text from sm up.
              'flex min-h-6 max-w-56 items-center gap-1.5 rounded-sm px-1 text-[11px] hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring sm:min-h-0',
              shown ? 'text-foreground' : 'text-muted-foreground line-through',
            )}
          >
            <span
              className={cn('shrink-0', form === 'line' ? 'h-0.5 w-3 rounded-full' : 'h-2 w-2 rounded-[2px]', !shown && 'opacity-40')}
              style={{ background: entry.color }}
            />
            <span className="truncate">{entry.name}</span>
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
  const names = model.legend.map(e => e.name).join(', ')
  const columns = model.panels.length > 1 ? ` in panels of ${model.panels.map(p => p.columns.join(' and ')).join('; ')}` : ''
  const over = model.kind === 'time' ? 'over time' : `by ${model.category}`
  return `${form === 'line' ? 'Line' : form === 'area' ? 'Area' : 'Bar'} chart of ${names} ${over}${columns}`
}

/** Each table's number, so a new table, a new run's, starts with every series shown. */
const tableIds = new WeakMap<LogTable, number>()
let lastTableId = 0
function tableKey(table: LogTable): number {
  let id = tableIds.get(table)
  if (id === undefined) tableIds.set(table, (id = ++lastTableId))
  return id
}

