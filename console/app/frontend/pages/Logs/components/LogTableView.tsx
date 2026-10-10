import { useMemo, useRef } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { format } from 'date-fns'
import { pivotsOn, type LogTable, type TableColumn } from '@/lib/logs/table'
import type { AttributeValue } from '@/lib/logs/stream'
import { cn } from '@/lib/utils'
import { Tag, valueText } from './TagMenu'

const ROW_HEIGHT = 24
const CHAR_PX = 7.5
const MIN_WIDTH = 64
const MAX_WIDTH = 420
/** A cell's padding (px-3) and its tag's (px-0.5), around the text. */
const CELL_PADDING = 24 + 4
/** Rows read to size the columns: enough to see their usual width. */
const SIZING_ROWS = 200

/**
 * A tabular Query's result, in place of the stream, virtualised. Cells of
 * Field-like columns are tags, so Show only this and Hide this pivot from them.
 */
export function LogTableView({ table }: { table: LogTable }) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const widths = useMemo(() => columnWidths(table), [table])
  const total = widths.reduce((sum, w) => sum + w, 0)
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
        <div className="sticky top-0 z-10 flex border-b bg-muted text-left text-muted-foreground">
          {table.columns.map((c, i) => (
            <div key={i} className="shrink-0 truncate px-3 py-1.5 font-medium" style={{ width: widths[i] }} title={`${c.name} (${c.type})`}>
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
            >
              {table.columns.map((c, i) => (
                <div key={i} className="shrink-0 truncate px-3" style={{ width: widths[i] }}>
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

function Cell({ column, value }: { column: TableColumn; value: AttributeValue }) {
  if (value === null || value === undefined) return <span className="text-muted-foreground/60">—</span>
  return (
    <Tag field={column.name} value={value} filterable={pivotsOn(column)}>
      {column.type === 'datetime' && typeof value === 'string'
        ? <span title={value}>{format(new Date(value), 'MMM dd HH:mm:ss.SSS')}</span>
        : valueText(value, 200)}
    </Tag>
  )
}

/** Each column wide enough for its name and its usual value, within bounds. */
function columnWidths(table: LogTable): number[] {
  return table.columns.map((c, i) => {
    let chars = c.name.length
    for (const row of table.rows.slice(0, SIZING_ROWS)) {
      const v = row[i]
      const text = c.type === 'datetime' ? 'MMM dd HH:mm:ss.SSS' : v === null || v === undefined ? '' : valueText(v as AttributeValue)
      chars = Math.max(chars, text.length)
    }
    return Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, Math.ceil(chars * CHAR_PX) + CELL_PADDING))
  })
}
