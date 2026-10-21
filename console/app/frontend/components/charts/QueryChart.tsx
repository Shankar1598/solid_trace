import { useCallback, useMemo, useState } from 'react'
import { format } from 'date-fns'
import { Area, AreaChart, Bar, BarChart, CartesianGrid, LabelList, Line, LineChart, ReferenceArea, ReferenceLine, XAxis, YAxis } from 'recharts'
import type { LabelListEntry, MouseHandlerDataParam } from 'recharts'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import {
  chartOf, fadedSpans, formFor, keepColors, keepHidden, legendNotes,
  type ChartForm, type ChartPoint, type Series, type SeriesChartModel, type SplitValue,
} from '@/lib/charts/series'
import {
  CHAR_PX, LABEL_PAD_PX, nameChars, optionsText, shortName, statText, tickLabel, ticksInside, timePattern, timeTicks, valueText, valueTicks, withUnit,
  type ValueOptions,
} from '@/lib/charts/axis'
import { binSize, binSpan } from '@/lib/charts/bins'
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

/**
 * The series ids hidden from the legend of the last chart drawn in one
 * place, kept by its owner across tables so a re-run or a zoom shows none of
 * them again.
 */
export interface HiddenSeries {
  current: ReadonlySet<string>
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
  /** Keeps the series hidden from the last table drawn here; without it, each new table shows every series. */
  hidden?: HiddenSeries
  /**
   * A control beside each legend entry that is one Field's value, such as
   * the explorer's value menu; without it, the legend only shows and hides.
   */
  legendValue?: (field: string, value: SplitValue) => React.ReactNode
  /** A Dashboard chart's options: stacked false never stacks; unit and decimals read the stat and tooltip values, and the unit the value axis. */
  options?: ChartOptions
  /** A single number's column name over it; off on a Dashboard, whose card title names it. */
  statName?: boolean
  /** A Monitor's threshold, drawn as a line across each panel, whose value axis reaches it. */
  threshold?: number
  className?: string
}

export interface ChartOptions extends ValueOptions {
  stacked?: boolean
}

/** The most time ticks a chart labels; fewer show when they would crowd. */
const TIME_TICKS = 6
/** Bars over more categories than this, or with longer names, lie across with their names beside them. */
const UPRIGHT_BARS = 8
const UPRIGHT_NAME = 12
/** A lying bar's band, and the gap between its tip and its value. */
const BAND_PX = 20
const TIP_GAP_PX = 4
/** The room right of the plot, where the last time label's tail can sit. */
const PLOT_RIGHT_PX = 12

/**
 * A Query's table drawn as a time series (line, area or bars), bars over
 * categories, or a single number. Columns that measure different things get
 * a panel each, over one time axis. The legend shows and hides series;
 * dragging across a time chart zooms to the bins dragged over. Used wherever
 * a Query's table is charted, so it takes the table and options, never a
 * page's state.
 */
export function QueryChart({ table, form, onZoom, actions, colors, hidden, legendValue, options, statName = true, threshold, className }: QueryChartProps) {
  const model = useMemo(() => {
    const made = chartOf(table)
    if (!colors) return made
    const kept = keepColors(made, colors.current)
    colors.current = kept.slots
    return kept.model
  }, [table, colors])
  // A table with no series, such as a single number, leaves the ones hidden for the next chart, as it does their colours.
  const hiddenNow = useMemo(() => {
    const kept = keepHidden(model, hidden?.current ?? new Set())
    if (hidden && (model.kind === 'time' || model.kind === 'bars')) hidden.current = kept
    return kept
  }, [model, hidden])
  const drawn = formFor(model, form ?? 'auto')
  if (model.kind === 'none' || drawn === null) return null
  if (model.kind === 'stat') {
    return (
      <div className={cn('flex items-start justify-between gap-3 px-3 py-2', className)}>
        <StatTile name={statName ? model.name : null} value={model.value} options={options} />
        {actions}
      </div>
    )
  }
  return (
    <SeriesChart
      key={tableKey(table)}
      model={model}
      form={drawn}
      initialHidden={hiddenNow}
      onHidden={hidden && (ids => { hidden.current = ids })}
      onZoom={onZoom}
      actions={actions}
      legendValue={legendValue}
      options={options}
      threshold={threshold}
      className={className}
    />
  )
}

/** One number, large, with its column's name over it when `name` is given. */
function StatTile({ name, value, options }: { name: string | null; value: number | null; options?: ValueOptions }) {
  return (
    <div className="min-w-0" data-chart-stat>
      {name !== null && <div className="truncate font-mono text-[11px] text-muted-foreground">{name}</div>}
      <div className="truncate font-mono text-3xl font-semibold leading-tight text-foreground tabular-nums" title={value === null ? undefined : String(value)}>
        {value === null ? '—' : optionsText(value, options ?? {}, statText)}
      </div>
    </div>
  )
}

/** The bins a drag has covered, by their points' indexes. */
type Drag = { start: number; end: number }

function SeriesChart({ model, form, initialHidden, onHidden, onZoom, actions, legendValue, options, threshold, className }: {
  model: SeriesChartModel
  form: ChartForm
  initialHidden: ReadonlySet<string>
  onHidden?: (ids: ReadonlySet<string>) => void
  onZoom?: QueryChartProps['onZoom']
  actions?: React.ReactNode
  legendValue?: QueryChartProps['legendValue']
  options?: ChartOptions
  threshold?: number
  className?: string
}) {
  const [hidden, setHidden] = useState<ReadonlySet<string>>(initialHidden)
  const [drag, setDrag] = useState<Drag | null>(null)
  const [measure, width] = useWidth()
  const config = useMemo<ChartConfig>(
    () => Object.fromEntries(model.series.map(s => [s.key, { label: s.name, color: s.color }])),
    [model],
  )
  const toggle = (id: string) => {
    const next = new Set(hidden)
    if (!next.delete(id)) next.add(id)
    setHidden(next)
    onHidden?.(next)
  }

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
  // Many categories, or long names, read best as bars lying across, names beside them, the chart as tall as they need:
  // the names take up to a share of its width, cut in the middle, and each bar's value shows at its tip.
  const names = model.kind === 'bars' ? model.points.map(p => String(p.x)) : []
  const across = names.length > UPRIGHT_BARS || names.some(n => n.length > UPRIGHT_NAME)
  const chars = Math.min(nameChars(width), Math.max(0, ...names.map(n => [...n].length)))
  const nameWidth = chars * CHAR_PX + LABEL_PAD_PX

  const several = model.panels.length > 1
  const panels = model.panels.map(panel => {
    const series = model.series.filter(s => panel.keys.includes(s.key))
    const visible = series.filter(s => !hidden.has(s.id))
    const stacked = panel.stacked && form !== 'line' && options?.stacked !== false
    const [low, high] = extent(model.points, visible, stacked)
    const reach = threshold === undefined ? [low, high] : [Math.min(low, threshold), Math.max(high, threshold)]
    const values = valueTicks(reach[0], reach[1], several ? 3 : 5)
    const tick = tickLabel(values.step)
    const tips = across && (stacked || visible.length === 1)
    const tipWidth = tips ? Math.max(0, ...model.points.map(p => tipText(p, visible).length)) * CHAR_PX + TIP_GAP_PX : 0
    return { panel, series, visible, stacked, values, label: (t: number) => withUnit(tick(t), options?.unit), tips, tipWidth }
  })
  const valueWidth = Math.max(...panels.map(({ values, label }) => Math.max(...values.ticks.map(t => label(t).length)))) * CHAR_PX + LABEL_PAD_PX
  // A time chart's right margin is PLOT_RIGHT_PX; its labels are 10px mono, centred on their ticks.
  const timeTickList = time ? timeTicks(time.from.getTime(), time.to.getTime(), TIME_TICKS) : []
  const timeLabels = time && domain && timeTickList.length > 0
    ? ticksInside(timeTickList, domain, [valueWidth, width - PLOT_RIGHT_PX], width, (format(new Date(timeTickList[0]), pattern).length * CHAR_PX) / 2)
    : timeTickList

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
  // A bin the range covers in part counts part of a bin, so it is faded, while such bins are few.
  const partialAreas = time && domain ? fadedSpans(model, form).map(([x1, x2]) => (
    <ReferenceArea
      key={x1}
      x1={Math.max(x1, domain[0])}
      x2={Math.min(x2, domain[1])}
      fill="var(--card)"
      fillOpacity={0.6}
      stroke="none"
      ifOverflow="hidden"
      zIndex={450}
    />
  )) : null

  const thresholdLine = threshold === undefined ? null : (
    <ReferenceLine
      {...(across ? { x: threshold } : { y: threshold })}
      stroke="var(--destructive)"
      strokeDasharray="4 3"
      ifOverflow="extendDomain"
      label={{ value: `threshold ${threshold}`, position: across ? 'top' : 'insideTopRight', fontSize: 10, fill: 'var(--destructive)' }}
    />
  )

  const height = !several ? 'h-44 sm:h-56' : panels.length === 2 ? 'h-32 sm:h-40' : 'h-28 sm:h-32'

  return (
    <div ref={measure} className={cn('flex flex-col', className)}>
      <div className="flex min-h-7 flex-wrap items-center gap-x-3 gap-y-1 px-3 pt-1.5" data-chart-legend>
        <Legend model={model} form={form} hidden={hidden} onToggle={toggle} legendValue={legendValue} />
        {(time || actions) && (
          <div className="ml-auto flex items-center gap-3">
            {time && <span className="font-mono text-[10px] text-muted-foreground" data-chart-bin>{binSize(time.binSeconds)} bins</span>}
            {actions}
          </div>
        )}
      </div>
      <div role="figure" aria-label={chartLabel(model, form)}>
        {panels.map(({ panel, series, stacked, values, label, tips, tipWidth }, i) => (
          <div key={panel.keys[0]}>
            {several && <div className="truncate px-3 pt-1 font-mono text-[10px] text-muted-foreground" data-chart-panel>{panel.columns.join(', ')}</div>}
            <ChartContainer
              config={config}
              className={cn('aspect-auto w-full px-1 font-mono', !across && height, zooms && 'cursor-crosshair select-none')}
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
                tipWidth={tips ? tipWidth : null}
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
                    ticks={timeLabels}
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
                    tick={<NameTick chars={chars} />}
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
                    content={<ChartTooltipContent indicator="line" valueFormatter={(value: number) => optionsText(value, options ?? {})} labelFormatter={(_, payload) => pointLabel(payload?.[0]?.payload?.start ?? payload?.[0]?.payload?.x)} />}
                  />
                )}
                overlay={<>{partialAreas}{dragArea}{thresholdLine}</>}
                events={zoomEvents}
              />
            </ChartContainer>
          </div>
        ))}
      </div>
    </div>
  )

  /** A point's bin or category, for the tooltip: a bin's span, start to end, in local time. */
  function pointLabel(x: unknown): string {
    if (!time) return String(x)
    const start = Number(x)
    return `${binSpan(start, time.binSeconds, time)}${time.partial.includes(start) ? ' · partial bin' : ''}`
  }
}

/**
 * A category's name beside its bar, on one line: cut in the middle to chars
 * with its full name as a title. recharts would wrap it to the axis' width instead.
 */
function NameTick({ x, y, payload, chars }: { x?: number; y?: number; payload?: { value: unknown }; chars: number }) {
  const name = String(payload?.value ?? '')
  return (
    <text x={x} y={y} dx={-4} dy={3} textAnchor="end" fontSize={10} className="fill-muted-foreground">
      <title>{name}</title>
      {shortName(name, chars)}
    </text>
  )
}

/** The series shown at a point that have a value there, bottom of the stack first. */
function valued(point: ChartPoint, series: Series[]): Series[] {
  return series.filter(s => typeof point[s.key] === 'number' && point[s.key] !== 0)
}

/** What a lying bar's tip reads: its value, or the sum of the series stacked in it. */
function tipText(point: ChartPoint, shown: Series[]): string {
  const values = valued(point, shown)
  return values.length === 0 ? '' : valueText(values.reduce((sum, s) => sum + (point[s.key] as number), 0))
}

/** One panel's chart: its series in the form asked for, over the axes given. */
function PanelChart({ form, data, series, hidden, stacked, model, across, tipWidth, axisBelow, xAxis, yAxis, tooltip, overlay, events }: {
  form: ChartForm
  data: ChartPoint[]
  series: Series[]
  hidden: ReadonlySet<string>
  stacked: boolean
  model: SeriesChartModel
  /** Bars lying across, categories down the side. */
  across: boolean
  /** The room a lying bar's value takes past its tip, or null when the bars show none. */
  tipWidth: number | null
  /** Whether the x axis shows below; a panel above another hides it, and keeps room for its lowest value's label. */
  axisBelow: boolean
  xAxis: React.ReactNode
  yAxis: React.ReactNode
  tooltip: React.ReactNode
  overlay: React.ReactNode
  events: object
}) {
  const stack = stacked ? 'stack' : undefined
  const shown = series.filter(s => !hidden.has(s.id))
  // A lying bar's value sits past the tip of its last segment with a value.
  const tip = (s: Series) => (entry: LabelListEntry) => {
    const point = entry.payload as ChartPoint
    return valued(point, shown).at(-1)?.key === s.key ? tipText(point, shown) : undefined
  }
  // Lines join across empty bins, so only a series' one value draws no line; it gets a dot.
  const lones = new Set(series.filter(s => model.points.filter(p => p[s.key] != null).length === 1).map(s => s.key))
  const dotFor = (s: Series) => (props: { cx?: number; cy?: number; index?: number }) => {
    const i = props.index ?? -1
    const lone = lones.has(s.key) && model.points[i]?.[s.key] != null
    return lone && props.cx !== undefined && props.cy !== undefined
      ? <circle key={`${s.key}-${i}`} cx={props.cx} cy={props.cy} r={4} fill={`var(--color-${s.key})`} stroke="var(--card)" strokeWidth={2} />
      : <g key={`${s.key}-${i}`} />
  }
  const right = tipWidth === null ? PLOT_RIGHT_PX : Math.max(PLOT_RIGHT_PX, tipWidth + TIP_GAP_PX)
  const chart = { data, margin: { top: 6, right, bottom: axisBelow ? 0 : 10, left: 0 }, ...events }
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
            stroke={stack ? 'var(--card)' : undefined}
            strokeWidth={stack ? 1 : 0}
            isAnimationActive={false}
          >
            {tipWidth !== null && (
              <LabelList
                valueAccessor={tip(s)}
                position="right"
                offset={TIP_GAP_PX}
                fontSize={10}
                fill="var(--muted-foreground)"
              />
            )}
          </Bar>
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
            connectNulls
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
          connectNulls
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
function Legend({ model, form, hidden, onToggle, legendValue }: {
  model: SeriesChartModel; form: ChartForm; hidden: ReadonlySet<string>; onToggle: (id: string) => void
  legendValue?: QueryChartProps['legendValue']
}) {
  const notes = legendNotes(model).map(({ text, why }) => (
    <span key={text} className="text-[11px] text-muted-foreground" title={why}>{text}</span>
  ))
  // An entry split by one Field's value gets the control for that value.
  const splits = new Map(model.series.map(s => [s.id, s.split]))
  const valueControl = (id: string) => {
    const split = splits.get(id)
    return legendValue && split?.length === 1 ? legendValue(split[0].field, split[0].value) : null
  }

  if (model.legend.length === 1) {
    return (
      <>
        {model.panels.length === 1 && <span className="truncate font-mono text-[11px] text-muted-foreground">{model.legend[0].name}</span>}
        {valueControl(model.legend[0].id)}
        {notes}
      </>
    )
  }
  return (
    <>
      {model.legend.map(entry => {
        const shown = !hidden.has(entry.id)
        return (
          <span key={entry.id} className="flex min-w-0 items-center">
            <button
              type="button"
              aria-pressed={shown}
              data-chart-series
              title={`${shown ? 'Hide' : 'Show'} ${entry.name}`}
              onClick={() => onToggle(entry.id)}
              className={cn(
                // 24px tall on a phone, a target a finger can hit; as dense as the text from sm up.
                'flex min-h-6 max-w-56 items-center gap-1.5 px-1 text-[11px] hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring sm:min-h-0',
                shown ? 'text-foreground' : 'text-muted-foreground line-through',
              )}
            >
              <span
                className={cn('shrink-0', form === 'line' ? 'h-0.5 w-3' : 'h-2 w-2', !shown && 'opacity-40')}
                style={{ background: entry.color }}
              />
              <span className="truncate font-mono">{entry.name}</span>
            </button>
            {valueControl(entry.id)}
          </span>
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

/** An element's width as it changes, from when it is laid out (0 before). */
function useWidth(): [(element: HTMLElement | null) => (() => void) | undefined, number] {
  const [width, setWidth] = useState(0)
  const measure = useCallback((element: HTMLElement | null) => {
    if (!element) return undefined
    setWidth(element.clientWidth)
    const observer = new ResizeObserver(([entry]) => setWidth(Math.round(entry.contentRect.width)))
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  return [measure, width]
}

/** Each table's number, so a new table, a new run's, starts afresh: the hidden series come from its owner. */
const tableIds = new WeakMap<LogTable, number>()
let lastTableId = 0
function tableKey(table: LogTable): number {
  let id = tableIds.get(table)
  if (id === undefined) tableIds.set(table, (id = ++lastTableId))
  return id
}

