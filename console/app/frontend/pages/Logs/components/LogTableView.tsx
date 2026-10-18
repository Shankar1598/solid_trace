import { useMemo, useRef } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { format } from 'date-fns'
import { ChevronDown } from 'lucide-react'
import { QueryChart, type ColorSlots, type HiddenSeries } from '@/components/charts/QueryChart'
import { chartOf, formFor, formsOf, keepColors, totalsColors, type SplitValue } from '@/lib/charts/series'
import type { AbsoluteRange, Viz } from '@/lib/logs/explorerState'
import { numberText, pivotsOn, type LogTable, type LogTotals, type TableColumn } from '@/lib/logs/table'
import type { AttributeValue } from '@/lib/logs/stream'
import { runLine, totalsLine } from '@/lib/logs/runLine'
import { cn } from '@/lib/utils'
import { ChartSwitch, type Shown } from './ChartSwitch'
import { Tag, valueText } from './TagMenu'

const ROW_HEIGHT = 24
const CHAR_PX = 7.5
const MIN_WIDTH = 64
const MAX_WIDTH = 420
/** A cell's padding (px-3) and its tag's (px-0.5), around the text. */
const CELL_PADDING = 24 + 4
/** Rows read to size the columns: enough to see their usual width. */
const SIZING_ROWS = 200
/** The column of a totals row's series colour. */
const SWATCH_WIDTH = 20

/** How the explorer draws a table: the chart asked for, and what changes it. */
export interface TableChart {
  viz: Viz
  onViz: (viz: Viz) => void
  /** Runs the explorer over the range dragged across a time chart. */
  onZoom: (range: AbsoluteRange) => void
  /** Each series' colour from the last chart, so a re-run repaints none. */
  colors: ColorSlots
  /** The series hidden from the last chart's legend, so a re-run or a zoom shows none of them again. */
  hidden: HiddenSeries
  /** Controls beside the switch for the form shown, such as Add to dashboard. */
  beside?: (shown: Shown) => React.ReactNode
}

/**
 * A tabular Query's result, in place of the stream. A table that can be drawn
 * shows its chart above it, with a switch between the chart's forms and the
 * table alone; the two scroll together when the room is short, as on a phone.
 * Under a time chart, the rows are the table's totals when the run has them:
 * a row per group over the range, with its series' colour, so the table
 * reads as the legend.
 */
export function LogTableView({ table, elapsedMs = null, chart }: { table: LogTable; elapsedMs?: number | null; chart?: TableChart }) {
  const model = useMemo(() => chartOf(table), [table])
  const colors = chart?.colors
  // The colours QueryChart gives the series from the same slots; it keeps them for the next table.
  const swatches = useMemo(
    () => (table.totals && colors ? totalsColors(keepColors(model, colors.current).model, table.totals) : null),
    [model, table.totals, colors],
  )
  const forms = formsOf(model)
  const line = <RunLine text={runLine(table.rows.length, 'row', elapsedMs)} />
  if (!chart || forms.length === 0) {
    return (
      <div className="flex h-full flex-col">
        {chart?.beside && <div className="flex justify-end px-3 py-1.5">{chart.beside('table')}</div>}
        {line}
        <div className="min-h-0 flex-1"><TableRows table={table} /></div>
      </div>
    )
  }

  const form = formFor(model, chart.viz)
  const totals = form && model.kind === 'time' ? table.totals : undefined
  const grouped = model.kind === 'time' && model.series.some(s => s.split.length > 0)
  // The shape's own form (Line over time, Bars over categories, a Number)
  // leaves no trace in the URL, so it never carries over to another shape.
  const onShow = (shown: Shown) => chart.onViz(shown === forms[0] || shown === 'stat' ? 'auto' : shown)
  const control = (
    <div className="flex items-center gap-2">
      {chart.beside?.(form ?? 'table')}
      <ChartSwitch forms={forms} shown={form ?? 'table'} onChange={onShow} />
    </div>
  )
  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className={cn('shrink-0', form !== 'stat' && 'border-b')}>
        {form
          ? <QueryChart table={table} form={form} onZoom={chart.onZoom} colors={chart.colors} hidden={chart.hidden} actions={control} legendValue={legendValue} className="pb-1" />
          : <div className="flex justify-end px-3 py-1.5">{control}</div>}
      </div>
      {totals ? <RunLine text={totalsLine(grouped ? totals.rows.length : null, elapsedMs)} /> : line}
      {/* A single number is its table: Table shows the cell. */}
      {form !== 'stat' && (
        <div className="min-h-48 flex-1">
          {totals ? <TableRows table={totals} swatches={swatches} /> : <TableRows table={table} />}
        </div>
      )}
    </div>
  )
}

/**
 * The table's rows, virtualised. Cells of Field-like columns are tags, so Show
 * only this and Hide this pivot from them. With swatches, each row starts
 * with its series' colour.
 */
function TableRows({ table, swatches = null }: { table: LogTable | LogTotals; swatches?: (string | null)[] | null }) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const widths = useMemo(() => columnWidths(table), [table])
  const swatched = swatches?.some(color => color !== null) ?? false
  const total = widths.reduce((sum, w) => sum + w, swatched ? SWATCH_WIDTH : 0)
  const virtualizer = useVirtualizer({
    count: table.rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 30,
    useFlushSync: false,
  })

  if (table.rows.length === 0) {
    return <div className="p-10 text-center text-sm text-muted-foreground">No rows. Widen the time range or change the Query.</div>
  }

  return (
    <div ref={scrollRef} className="h-full overflow-auto font-mono text-[12px]">
      <div style={{ minWidth: total }}>
        <div className="sticky top-0 z-10 flex border-b bg-card text-left text-muted-foreground">
          {swatched && <div className="shrink-0" style={{ width: SWATCH_WIDTH }} />}
          {table.columns.map((c, i) => (
            <div key={i} className={cn('shrink-0 truncate px-3 py-1.5 font-medium', c.type === 'number' && 'text-right')} style={{ width: widths[i] }} title={`${c.name} (${c.type})`}>
              {c.name}
            </div>
          ))}
        </div>
        <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
          {virtualizer.getVirtualItems().map(item => (
            <div
              key={item.key}
              className={cn('absolute left-0 top-0 flex w-full items-center', item.index % 2 === 1 && 'bg-muted/30')}
              style={{ height: ROW_HEIGHT, transform: `translateY(${item.start}px)` }}
              data-table-row
            >
              {swatched && (
                <div className="flex shrink-0 justify-end" style={{ width: SWATCH_WIDTH }}>
                  {swatches![item.index] && <span className="h-2 w-2" style={{ background: swatches![item.index]! }} data-swatch />}
                </div>
              )}
              {table.columns.map((c, i) => (
                <div key={i} className={cn('shrink-0 truncate px-3', c.type === 'number' && 'text-right')} style={{ width: widths[i] }}>
                  <Cell column={c} value={table.rows[item.index][i] as AttributeValue} />
                </div>
              ))}
            </div>
          ))}
        </div>
        {table.truncated && (
          <div className="px-3 py-1 text-center font-sans text-xs text-muted-foreground">
            Showing the first {table.rows.length.toLocaleString()} rows. Add a take or narrow the Query to see the rest.
          </div>
        )}
      </div>
    </div>
  )
}

/** What the run found and how long it took: "12 rows · 230 ms". */
function RunLine({ text }: { text: string }) {
  return (
    <div className="shrink-0 border-b px-3 py-1 text-[11px] text-muted-foreground tabular-nums" data-run-line>
      {text}
    </div>
  )
}

/** A legend entry's value menu: the entry itself shows and hides its series. */
function legendValue(field: string, value: SplitValue) {
  return (
    <Tag field={field} value={value} label={`Menu for ${field} = ${valueText(value, 40)}`} className="flex h-5 items-center px-0.5 text-muted-foreground hover:text-foreground">
      <ChevronDown className="h-3 w-3" />
    </Tag>
  )
}

function Cell({ column, value }: { column: TableColumn; value: AttributeValue }) {
  if (value === null || value === undefined) return <span className="text-muted-foreground/60">—</span>
  return (
    <Tag field={column.name} value={value} filterable={pivotsOn(column)}>
      {column.type === 'datetime' && typeof value === 'string'
        ? <span title={value}>{format(new Date(value), 'MMM dd HH:mm:ss.SSS')}</span>
        : typeof value === 'number'
          ? <span title={String(value)}>{numberText(column, value)}</span>
          : valueText(value, 200)}
    </Tag>
  )
}

/** Each column wide enough for its name and its usual value, within bounds. */
function columnWidths(table: LogTable | LogTotals): number[] {
  return table.columns.map((c, i) => {
    let chars = c.name.length
    for (const row of table.rows.slice(0, SIZING_ROWS)) {
      const v = row[i]
      const text = c.type === 'datetime' ? 'MMM dd HH:mm:ss.SSS'
        : v === null || v === undefined ? ''
        : typeof v === 'number' ? numberText(c, v)
        : valueText(v as AttributeValue)
      chars = Math.max(chars, text.length)
    }
    return Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, Math.ceil(chars * CHAR_PX) + CELL_PADDING))
  })
}
