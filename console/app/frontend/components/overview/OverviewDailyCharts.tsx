import { useMemo } from 'react'
import { Bar, BarChart, CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { statText, tickLabel, valueText, valueTicks } from '@/lib/charts/axis'
import { INK, bytesText, bytesTicks, chartColor, dayText, rateText, secondText, type Overview } from '@/lib/overview'
import { cn } from '@/lib/utils'

type Daily = NonNullable<Overview['daily']>

/**
 * The Overview's daily Charts, each with its table below: Logs per day
 * stacked by Project, received and stored bytes as grouped bars on one
 * axis, and the average and peak Logs per second as two lines. Two across
 * on wide screens, stacked on a phone.
 */
export function OverviewDailyCharts({ daily }: { daily: Daily }) {
  const { series, days } = daily
  const logs = useMemo(() => days.map(d => ({ day: d.day, ...d.logs })), [days])
  const bytes = useMemo(() => days.map(d => ({ day: d.day, received: d.received_bytes, stored: d.stored_bytes })), [days])
  const rates = useMemo(() => days.map(d => ({ day: d.day, average: d.logs_per_second.average, peak: d.logs_per_second.peak })), [days])
  const peakAt = new Map(days.map(d => [d.day, d.logs_per_second.peak_at]))

  return (
    <div className="grid gap-x-10 gap-y-12 lg:grid-cols-2">
      <DailyChart
        id="logs"
        title="Logs per day"
        description="Logs received, by Project."
        form="stacked"
        series={series.map(s => ({ key: s.key, name: s.name, color: chartColor(s.color) }))}
        data={logs}
        value={statText}
        exact={valueText}
      />
      <DailyChart
        id="bytes"
        title="Bytes per day"
        description="Received, as decoded, beside stored, as ClickHouse keeps it."
        form="grouped"
        series={[
          { key: 'received', name: 'Received', color: INK.light },
          { key: 'stored', name: 'Stored', color: INK.full },
        ]}
        data={bytes}
        value={bytesText}
        bytes
      />
      <DailyChart
        id="rates"
        title="Logs per second"
        description="Each day's average, and its busiest second."
        form="lines"
        series={[
          { key: 'peak', name: 'Peak', color: INK.full },
          { key: 'average', name: 'Average', color: INK.mid },
        ]}
        data={rates}
        value={rateText}
        note={day => (peakAt.get(day) ? `peak at ${secondText(peakAt.get(day)!)}` : null)}
      />
    </div>
  )
}

interface DailySeries {
  key: string
  name: string
  color: string
}

type DailyPoint = { day: string } & Record<string, number | string>

/** The plot's height, the room under it for the day axis, and over it. */
const HEIGHT_PX = 224
const X_AXIS_PX = 24
const TOP_PX = 8
/** The most series labelled directly, at the right of the plot; past it, the legend alone names them. */
const DIRECT_LABELS = 4
const LABEL_GUTTER_PX = 84
/** The least room a direct label takes, up and down. */
const LABEL_PX = 14

/**
 * One daily Chart: its legend, its plot over the range's days, one value
 * axis, a tooltip on each day, and its table below. Two to four series are
 * also labelled directly at the right, beside the last day's marks; a single
 * series needs neither labels nor a legend.
 */
function DailyChart({ id, title, description, form, series, data, value, exact = value, bytes = false, note }: {
  id: string
  title: string
  description: string
  form: 'stacked' | 'grouped' | 'lines'
  series: DailySeries[]
  data: DailyPoint[]
  /** A value as the axis, table and labels write it. */
  value: (n: number) => string
  /** A value as the tooltip writes it. */
  exact?: (n: number) => string
  bytes?: boolean
  /** A line under a day's tooltip. */
  note?: (day: string) => string | null
}) {
  const config = useMemo<ChartConfig>(() => Object.fromEntries(series.map(s => [s.key, { label: s.name, color: s.color }])), [series])
  const num = (p: DailyPoint, key: string) => (typeof p[key] === 'number' ? (p[key] as number) : 0)
  const high = Math.max(0, ...data.map(p => (form === 'stacked' ? series.reduce((sum, s) => sum + num(p, s.key), 0) : Math.max(...series.map(s => num(p, s.key))))))
  const axis = useMemo(() => {
    if (bytes) return bytesTicks(high)
    const ticks = valueTicks(0, high, 4)
    return { ...ticks, label: tickLabel(ticks.step) }
  }, [bytes, high])
  const labelled = series.length > 1 && series.length <= DIRECT_LABELS && data.length > 0
  const labels = labelled ? directLabels(series, data.at(-1)!, form, axis.domain[1], num) : []

  return (
    <figure className="min-w-0 space-y-3" data-chart={id}>
      <figcaption className="space-y-0.5">
        <div className="text-sm font-medium">{title}</div>
        <div className="text-xs text-muted-foreground">{description}</div>
      </figcaption>
      {series.length > 1 && (
        <ul className="flex flex-wrap gap-x-3 gap-y-1 text-xs" aria-label={`${title} legend`} data-chart-legend>
          {series.map(s => (
            <li key={s.key} className="flex items-center gap-1.5">
              <span className={cn('shrink-0', form === 'lines' ? 'h-0.5 w-3' : 'h-2 w-2')} style={{ background: s.color }} aria-hidden />
              {s.name}
            </li>
          ))}
        </ul>
      )}
      <div className="relative" role="img" aria-label={`${title}: ${series.map(s => s.name).join(', ')} by day`}>
        <ChartContainer config={config} className="aspect-auto w-full" style={{ height: HEIGHT_PX }}>
          {form === 'lines' ? (
            <LineChart data={data} margin={{ top: TOP_PX, right: labelled ? LABEL_GUTTER_PX : 12, bottom: 0, left: 0 }}>
              <Grid />
              <DayAxis />
              <ValueAxis axis={axis} />
              <ChartTooltip
                isAnimationActive={false}
                cursor={{ stroke: 'var(--muted-foreground)', strokeWidth: 1 }}
                content={<Tooltip value={exact} note={note} />}
              />
              {series.map(s => (
                <Line
                  key={s.key}
                  dataKey={s.key}
                  name={s.name}
                  type="linear"
                  stroke={`var(--color-${s.key})`}
                  strokeWidth={2}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  // A single day draws no line, so its point gets a dot.
                  dot={data.length === 1 ? { r: 4, fill: `var(--color-${s.key})`, stroke: 'var(--card)', strokeWidth: 2 } : false}
                  activeDot={{ r: 4, stroke: 'var(--card)', strokeWidth: 2 }}
                  isAnimationActive={false}
                />
              ))}
            </LineChart>
          ) : (
            <BarChart data={data} margin={{ top: TOP_PX, right: labelled ? LABEL_GUTTER_PX : 12, bottom: 0, left: 0 }} barCategoryGap="20%">
              <Grid />
              <DayAxis />
              <ValueAxis axis={axis} />
              <ChartTooltip
                isAnimationActive={false}
                cursor={{ fill: 'var(--muted)', opacity: 0.5 }}
                content={<Tooltip value={exact} note={note} />}
              />
              {series.map(s => (
                <Bar
                  key={s.key}
                  dataKey={s.key}
                  name={s.name}
                  stackId={form === 'stacked' ? 'logs' : undefined}
                  fill={`var(--color-${s.key})`}
                  maxBarSize={24}
                  // As QueryChart's: square, with a surface line between stacked segments.
                  stroke={form === 'stacked' ? 'var(--card)' : undefined}
                  strokeWidth={form === 'stacked' ? 1 : 0}
                  isAnimationActive={false}
                />
              ))}
            </BarChart>
          )}
        </ChartContainer>
        {labels.length > 0 && (
          <ul className="pointer-events-none absolute right-0 top-0 h-full text-[11px] text-muted-foreground" style={{ width: LABEL_GUTTER_PX - 8 }} aria-hidden data-chart-labels>
            {labels.map(l => (
              <li key={l.key} className="absolute left-0 flex w-full items-center gap-1" style={{ top: l.y - LABEL_PX / 2, height: LABEL_PX }}>
                <span className="h-0.5 w-2 shrink-0" style={{ background: l.color }} />
                <span className="truncate">{l.name}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
      <DailyTable title={title} series={series} data={data} value={value} />
    </figure>
  )
}

function Grid() {
  return <CartesianGrid vertical={false} stroke="var(--border)" />
}

function DayAxis() {
  return (
    <XAxis
      dataKey="day"
      height={X_AXIS_PX}
      tickLine={false}
      axisLine={{ stroke: 'var(--border)' }}
      interval="preserveStartEnd"
      minTickGap={16}
      tick={{ fontSize: 11 }}
      tickFormatter={dayText}
    />
  )
}

function ValueAxis({ axis }: { axis: { domain: [number, number]; ticks: number[]; label: (n: number) => string } }) {
  const width = Math.max(...axis.ticks.map(t => axis.label(t).length)) * 7 + 12
  return (
    <YAxis
      width={width}
      domain={axis.domain}
      ticks={axis.ticks}
      interval={0}
      tickLine={false}
      axisLine={false}
      tick={{ fontSize: 11 }}
      tickFormatter={axis.label}
    />
  )
}

interface TooltipProps {
  active?: boolean
  payload?: { payload?: DailyPoint }[]
  label?: string
  value: (n: number) => string
  note?: (day: string) => string | null
}

/** A day's tooltip: its date and each series' value, the day's note under them. */
function Tooltip({ value, note, ...props }: TooltipProps) {
  const day = props.payload?.[0]?.payload?.day
  const extra = day && note ? note(day) : null
  return (
    <div className="flex flex-col">
      <ChartTooltipContent
        {...(props as object)}
        indicator={'dot'}
        labelFormatter={() => (day ? dayText(day) : '')}
        valueFormatter={value}
      />
      {extra && <div className="-mt-1 border border-t-0 bg-background px-2.5 pb-1.5 text-xs text-muted-foreground">{extra}</div>}
    </div>
  )
}

/**
 * Where each series' direct label sits: beside the last day's mark, at the
 * middle of its segment when stacked, at its end otherwise, pushed apart so
 * none overlap. A segment too thin to hold a label leaves it to the legend.
 */
function directLabels(series: DailySeries[], last: DailyPoint, form: 'stacked' | 'grouped' | 'lines', max: number, num: (p: DailyPoint, key: string) => number) {
  const plot = HEIGHT_PX - X_AXIS_PX - TOP_PX
  const y = (v: number) => TOP_PX + plot * (1 - (max > 0 ? v / max : 0))
  let below = 0
  const placed = series.flatMap(s => {
    const v = num(last, s.key)
    if (form === 'stacked') {
      const start = below
      below += v
      if (y(start) - y(below) < LABEL_PX) return []
      return [{ ...s, y: (y(start) + y(below)) / 2 }]
    }
    return [{ ...s, y: y(v) }]
  }).sort((a, b) => a.y - b.y)
  for (let i = 1; i < placed.length; i++) placed[i].y = Math.max(placed[i].y, placed[i - 1].y + LABEL_PX)
  return placed
}

/** A daily Chart's numbers, a row per day, so nothing rests on colour alone. */
function DailyTable({ title, series, data, value }: { title: string; series: DailySeries[]; data: DailyPoint[]; value: (n: number) => string }) {
  return (
    <div className="max-h-56 overflow-y-auto border" data-chart-table>
      <Table aria-label={`${title} by day`}>
        <TableHeader className="sticky top-0 bg-background">
          <TableRow>
            <TableHead className="h-8 text-xs">Day</TableHead>
            {series.map(s => <TableHead key={s.key} className="h-8 text-right text-xs">{s.name}</TableHead>)}
          </TableRow>
        </TableHeader>
        <TableBody>
          {[...data].reverse().map(p => (
            <TableRow key={p.day}>
              <TableCell className="py-1 text-xs">{dayText(p.day)}</TableCell>
              {series.map(s => (
                <TableCell key={s.key} className="py-1 text-right font-mono text-xs tabular-nums" title={typeof p[s.key] === 'number' ? valueText(p[s.key] as number) : undefined}>
                  {typeof p[s.key] === 'number' ? value(p[s.key] as number) : '—'}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
