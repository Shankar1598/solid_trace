import { useEffect, useMemo, useRef, useState } from 'react'
import { Popover } from '@base-ui/react/popover'
import { useVirtualizer } from '@tanstack/react-virtual'
import { format } from 'date-fns'
import { ChevronDown, Columns3, X } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import { columnValue, moveColumn, pickableFields, toggleColumn } from '@/lib/logs/columns'
import type { AttributeValue, Log } from '@/lib/logs/stream'
import { cn } from '@/lib/utils'
import { Tag, valueText } from './TagMenu'

const ROW_HEIGHT = 30
/** The gutter before the columns, holding each row's Level square. */
const GUTTER = 22
/** The header's last cell, holding the column picker. */
const PICKER = 44
/** Start loading older Logs this many lines before the end. */
const LOAD_AHEAD = 100
const MIN_WIDTH = 48
const DEFAULT_WIDTH = 140
// time leaves room for its cell's menu button after the text.
const WIDTHS: Record<string, number> = { time: 200, level: 80, service: 120, environment: 120, message: 520, trace_id: 280, span_id: 170 }
/** Column widths a reader dragged, kept in their browser by field name. */
const WIDTHS_KEY = 'solid-trace.logs.column-widths'
/** The message column takes the room left over, so a wide screen shows more of each message. */
const GROWS = 'message'

export const LEVEL_TEXT: Record<string, string> = {
  trace: 'text-muted-foreground',
  debug: 'text-muted-foreground',
  info: 'text-sky-600 dark:text-sky-400',
  warn: 'text-amber-600 dark:text-amber-400',
  error: 'text-red-600 dark:text-red-400',
  fatal: 'text-red-700 dark:text-red-300 font-bold',
}

const LEVEL_SQUARE: Record<string, string> = {
  trace: 'bg-muted-foreground/40',
  debug: 'bg-muted-foreground/40',
  info: 'bg-sky-500',
  warn: 'bg-amber-500',
  error: 'bg-red-500',
  fatal: 'bg-red-800 ring-1 ring-red-500',
}

interface LogStreamProps {
  logs: Log[]
  end: 'more' | 'end' | 'capped'
  loadingOlder: boolean
  olderError: string | null
  onLoadOlder: () => void
  /** The Log open in the inspector. */
  selectedId: string | null
  onSelect: (log: Log) => void
  columns: string[]
  onColumns: (columns: string[]) => void
}

/**
 * The Logs of a run as a table, newest first, virtualised. The reader picks,
 * orders and sizes the columns; each row starts with a square in its Level's
 * colour. Clicking a row opens the inspector, whose lines add to the Query;
 * a cell's button opens the value menu. From the keyboard the stream is one
 * tab stop, at the open Log or the first: the arrows move between rows and
 * Enter or Space opens one.
 */
export function LogStream({ logs, end, loadingOlder, olderError, onLoadOlder, selectedId, onSelect, columns, onColumns }: LogStreamProps) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [widths, setWidths] = useState<Record<string, number>>(readWidths)
  const virtualizer = useVirtualizer({
    count: logs.length + 1, // the last row says what comes after the Logs
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 30,
    useFlushSync: false,
  })
  const items = virtualizer.getVirtualItems()
  const lastIndex = items.length > 0 ? items[items.length - 1].index : 0

  useEffect(() => {
    if (end === 'more' && !loadingOlder && !olderError && lastIndex >= logs.length - LOAD_AHEAD) onLoadOlder()
  }, [end, loadingOlder, olderError, lastIndex, logs.length, onLoadOlder])

  // The Log the inspector steps to scrolls into view, clear of the sticky header,
  // and so does a selected Log once an older page brings it (Logs around this one).
  const logsNow = useRef(logs)
  logsNow.current = logs
  const selectedShown = selectedId !== null && logs.some(l => l.id === selectedId)
  useEffect(() => {
    const box = scrollRef.current
    const at = selectedId === null ? -1 : logsNow.current.findIndex(l => l.id === selectedId)
    if (!box || at < 0) return
    const header = box.querySelector<HTMLElement>('[data-stream-header]')?.offsetHeight ?? 0
    const top = header + at * ROW_HEIGHT
    if (top < box.scrollTop + header) box.scrollTop = top - header
    else if (top + ROW_HEIGHT > box.scrollTop + box.clientHeight) box.scrollTop = top + ROW_HEIGHT - box.clientHeight
  }, [selectedId, selectedShown])

  // The row that takes Tab: the one last focused, else the open Log, else the first.
  const [focused, setFocused] = useState<number | null>(null)
  const selectedIndex = selectedId === null ? -1 : logs.findIndex(l => l.id === selectedId)
  const tabStop = focused !== null && focused < logs.length ? focused : Math.max(0, selectedIndex)
  const focusAfterRender = useRef<number | null>(null)
  const moveFocus = (index: number) => {
    const next = Math.min(logs.length - 1, Math.max(0, index))
    setFocused(next)
    focusAfterRender.current = next
    virtualizer.scrollToIndex(next)
  }
  // An arrow can move to a row the virtualiser hasn't drawn yet; it's focused once drawn.
  useEffect(() => {
    const index = focusAfterRender.current
    const row = index === null ? null : scrollRef.current?.querySelector<HTMLElement>(`[data-row-index="${index}"]`)
    if (row) {
      row.focus({ preventScroll: true })
      focusAfterRender.current = null
    }
  })

  const widthOf = (column: string) => widths[column] ?? WIDTHS[column] ?? DEFAULT_WIDTH
  const minWidth = GUTTER + columns.reduce((sum, c) => sum + widthOf(c), 0) + PICKER
  const resize = (column: string, width: number, done: boolean) => {
    const next = { ...widths, [column]: Math.max(MIN_WIDTH, Math.round(width)) }
    setWidths(next)
    if (done) saveWidths(next)
  }

  return (
    <div ref={scrollRef} role="grid" aria-label="Logs" aria-rowcount={logs.length + 1} className="h-full overflow-auto font-mono text-[13px]">
      <div style={{ minWidth }}>
        <HeaderRow columns={columns} widthOf={widthOf} onResize={resize} onColumns={onColumns} logs={logs} />
        <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
          {items.map(item => (
            <div
              key={item.key}
              className="absolute left-0 top-0 w-full"
              style={{ height: ROW_HEIGHT, transform: `translateY(${item.start}px)` }}
            >
              {item.index < logs.length
                ? (
                  <LogRow
                    log={logs[item.index]}
                    index={item.index}
                    columns={columns}
                    widthOf={widthOf}
                    selected={logs[item.index].id === selectedId}
                    tabStop={item.index === tabStop}
                    onSelect={onSelect}
                    onFocus={setFocused}
                    onMove={moveFocus}
                  />
                )
                : <StreamEnd end={end} loading={loadingOlder} error={olderError} count={logs.length} onRetry={onLoadOlder} />}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

function HeaderRow({ columns, widthOf, onResize, onColumns, logs }: {
  columns: string[]
  widthOf: (column: string) => number
  onResize: (column: string, width: number, done: boolean) => void
  onColumns: (columns: string[]) => void
  logs: Log[]
}) {
  const dragged = useRef<string | null>(null)
  const [dropBefore, setDropBefore] = useState<string | null>(null)

  /** The column a drop over `column` lands before: it, or the next one past its middle. */
  const target = (e: React.DragEvent, column: string) => {
    const box = e.currentTarget.getBoundingClientRect()
    if (e.clientX < box.left + box.width / 2) return column
    return columns[columns.indexOf(column) + 1] ?? null
  }

  return (
    <div data-stream-header role="row" aria-rowindex={1} className="sticky top-0 z-10 flex border-b bg-card text-left text-muted-foreground">
      <div aria-hidden className="shrink-0 border-r" style={{ width: GUTTER }} />
      {columns.map(column => (
        <div
          key={column}
          role="columnheader"
          className={cn('group relative flex shrink-0 items-center border-r', column === GROWS && 'grow',
            dropBefore === column && 'shadow-[inset_2px_0_0_var(--color-primary)]')}
          style={{ width: widthOf(column) }}
          onDragOver={e => {
            if (!dragged.current) return
            e.preventDefault()
            setDropBefore(target(e, column) ?? '')
          }}
          onDragLeave={() => setDropBefore(null)}
          onDrop={e => {
            e.preventDefault()
            if (dragged.current) onColumns(moveColumn(columns, dragged.current, target(e, column)))
            dragged.current = null
            setDropBefore(null)
          }}
        >
          <span
            draggable
            onDragStart={e => {
              dragged.current = column
              e.dataTransfer.effectAllowed = 'move'
              e.dataTransfer.setData('text/plain', column)
            }}
            onDragEnd={() => {
              dragged.current = null
              setDropBefore(null)
            }}
            className="min-w-0 flex-1 cursor-grab truncate px-2.5 py-2 active:cursor-grabbing"
            title={`${column}: drag to move`}
          >
            {column}
          </span>
          {columns.length > 1 && (
            <button
              onClick={() => onColumns(toggleColumn(columns, column))}
              className="absolute right-1.5 hidden bg-muted p-0.5 hover:bg-background group-hover:block"
              aria-label={`Hide the ${column} column`}
            >
              <X className="h-3 w-3" />
            </button>
          )}
          <ResizeHandle column={column} width={widthOf(column)} onResize={onResize} />
        </div>
      ))}
      <div
        role="columnheader"
        className={cn('sticky right-0 ml-auto flex shrink-0 items-center justify-end bg-card pr-2.5', dropBefore === '' && 'shadow-[inset_2px_0_0_var(--color-primary)]')}
        style={{ width: PICKER }}
        onDragOver={e => {
          if (!dragged.current) return
          e.preventDefault()
          setDropBefore('')
        }}
        onDrop={e => {
          e.preventDefault()
          if (dragged.current) onColumns(moveColumn(columns, dragged.current, null))
          dragged.current = null
          setDropBefore(null)
        }}
      >
        <ColumnPicker columns={columns} logs={logs} onColumns={onColumns} />
      </div>
    </div>
  )
}

/** The right edge of a header cell: drag it to size the column. */
function ResizeHandle({ column, width, onResize }: {
  column: string; width: number; onResize: (column: string, width: number, done: boolean) => void
}) {
  const start = useRef<{ x: number; width: number } | null>(null)
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={`Resize the ${column} column`}
      className="absolute inset-y-0 -right-1 z-10 w-2 cursor-col-resize after:absolute after:inset-y-1.5 after:left-[3px] after:w-px after:bg-border hover:after:bg-primary"
      onPointerDown={e => {
        e.preventDefault()
        e.currentTarget.setPointerCapture(e.pointerId)
        start.current = { x: e.clientX, width: e.currentTarget.parentElement?.getBoundingClientRect().width ?? width }
      }}
      onPointerMove={e => {
        if (start.current) onResize(column, start.current.width + e.clientX - start.current.x, false)
      }}
      onPointerUp={e => {
        if (start.current) onResize(column, start.current.width + e.clientX - start.current.x, true)
        start.current = null
      }}
      onDoubleClick={() => onResize(column, WIDTHS[column] ?? DEFAULT_WIDTH, true)}
    />
  )
}

/** Shows and hides columns: the built-in fields, the Attributes in view, or any Field by name. */
function ColumnPicker({ columns, logs, onColumns }: { columns: string[]; logs: Log[]; onColumns: (columns: string[]) => void }) {
  const [filter, setFilter] = useState('')
  const fields = useMemo(() => pickableFields(logs), [logs])
  const typed = filter.trim()
  const shown = fields.filter(f => f.toLowerCase().includes(typed.toLowerCase()))
  const extra = columns.filter(c => !fields.includes(c) && c.toLowerCase().includes(typed.toLowerCase()))
  const canAdd = typed !== '' && !fields.includes(typed) && !columns.includes(typed)

  return (
    <Popover.Root onOpenChange={open => !open && setFilter('')}>
      <Popover.Trigger className="p-1 hover:bg-background" aria-label="Choose columns" title="Choose columns">
        <Columns3 className="h-3.5 w-3.5" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner className="isolate z-50" side="bottom" align="end" sideOffset={4}>
          <Popover.Popup className="flex max-h-(--available-height) w-64 flex-col bg-popover font-sans text-xs text-popover-foreground shadow-md ring-1 ring-foreground/10 outline-none">
            <input
              autoFocus
              value={filter}
              onChange={e => setFilter(e.target.value)}
              onKeyDown={e => {
                if (e.key === 'Enter' && canAdd) {
                  onColumns(toggleColumn(columns, typed))
                  setFilter('')
                }
              }}
              placeholder="Find or add a Field…"
              className="border-b bg-transparent px-2.5 py-2 font-mono outline-none placeholder:font-sans placeholder:text-muted-foreground"
            />
            <div className="min-h-0 overflow-y-auto py-1">
              {canAdd && (
                <button
                  onClick={() => { onColumns(toggleColumn(columns, typed)); setFilter('') }}
                  className="flex w-full items-center px-2.5 py-1.5 text-left hover:bg-accent"
                >
                  Add <span className="mx-1 font-mono">{typed}</span> as a column
                </button>
              )}
              {[...extra, ...shown].map(field => (
                <label key={field} className="flex cursor-pointer items-center gap-2 px-2.5 py-1.5 hover:bg-accent">
                  <input
                    type="checkbox"
                    checked={columns.includes(field)}
                    disabled={columns.length === 1 && columns.includes(field)}
                    onChange={() => onColumns(toggleColumn(columns, field))}
                    className="accent-primary"
                  />
                  <span className="truncate font-mono">{field}</span>
                </label>
              ))}
              {!canAdd && shown.length + extra.length === 0 && <div className="px-2.5 py-1.5 text-muted-foreground">No Field matches.</div>}
            </div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}

/** One Log as a row; clicking it, or Enter or Space on it, opens the inspector. */
function LogRow({ log, index, columns, widthOf, selected, tabStop, onSelect, onFocus, onMove }: {
  log: Log
  index: number
  columns: string[]
  widthOf: (column: string) => number
  selected: boolean
  /** The stream's one tab stop. */
  tabStop: boolean
  onSelect: (log: Log) => void
  onFocus: (index: number) => void
  /** Focuses the row at that index, scrolling to it. */
  onMove: (index: number) => void
}) {
  const level = log.level ?? ''
  return (
    <div
      role="row"
      aria-rowindex={index + 2}
      aria-selected={selected}
      tabIndex={tabStop ? 0 : -1}
      data-row-index={index}
      // Selecting text to copy it doesn't open the inspector.
      onClick={() => { if (!window.getSelection()?.toString()) onSelect(log) }}
      onFocus={e => { if (e.target === e.currentTarget) onFocus(index) }}
      onKeyDown={e => {
        if (e.target !== e.currentTarget) return
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onSelect(log)
        } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
          e.preventDefault()
          onMove(index + (e.key === 'ArrowDown' ? 1 : -1))
        }
      }}
      className={cn(
        'flex h-full whitespace-nowrap border-b border-b-border/60 cursor-pointer outline-none hover:bg-muted/50',
        'focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring',
        selected && 'bg-primary/10 hover:bg-primary/15',
      )}
      // Room under the header's column picker, so a growing column ends where its header does.
      style={{ paddingRight: PICKER }}
    >
      <div aria-hidden className="flex shrink-0 items-center justify-center border-r" style={{ width: GUTTER }} title={level || undefined}>
        <span className={cn('h-2 w-2', LEVEL_SQUARE[level] ?? 'border border-muted-foreground/40')} />
      </div>
      {columns.map(column => (
        <CellBox key={column} log={log} column={column} width={widthOf(column)} />
      ))}
    </div>
  )
}

/** A row's cell; hovering it offers the value menu without opening the inspector. */
function CellBox({ log, column, width }: { log: Log; column: string; width: number }) {
  const value = columnValue(log, column)
  const empty = value === undefined || value === null || value === ''
  return (
    <div role="gridcell" className={cn('group/cell relative flex shrink-0 items-center border-r px-2.5', column === GROWS && 'grow')} style={{ width }}>
      <div className="min-w-0 truncate">{empty ? <span className="text-muted-foreground/50">—</span> : <Cell log={log} column={column} value={value} />}</div>
      {!empty && (
        <Tag
          field={column}
          value={column === 'time' ? log.time : value}
          columnable
          label={`Menu for ${column}`}
          className="absolute right-1 hidden items-center border bg-background px-1 py-0.5 text-foreground shadow-sm hover:bg-muted hover:outline-none group-hover/cell:flex data-popup-open:flex"
        >
          <ChevronDown className="h-3 w-3" />
        </Tag>
      )}
    </div>
  )
}

function Cell({ log, column, value }: { log: Log; column: string; value: AttributeValue }) {
  switch (column) {
    case 'time':
      return <span title={log.time} className="text-muted-foreground tabular-nums">{format(new Date(log.time), 'MMM dd HH:mm:ss.SSS')}</span>
    case 'level':
      return <span className={LEVEL_TEXT[level(value)]}>{valueText(value)}</span>
    case 'service':
      return <span>{valueText(value)}</span>
    case 'message':
      return <span title={log.message.length > 120 ? log.message.slice(0, 2000) : undefined}>{log.message}</span>
    default:
      return <span className="text-foreground/80">{valueText(value, 300)}</span>
  }
}

function level(value: AttributeValue): string {
  return typeof value === 'string' ? value : ''
}

/** The stream's placeholder while a run is on its way: rows the shape of Logs under a line saying what runs. */
export function StreamSkeleton({ label }: { label: string }) {
  return (
    <div role="status" className="h-full overflow-hidden">
      <div className="border-b px-3 py-2 text-xs text-muted-foreground">{label}</div>
      {SKELETON_MESSAGES.map((width, i) => (
        <div key={i} className="flex items-center gap-6 border-b border-b-border/60 px-2" style={{ height: ROW_HEIGHT }}>
          <Skeleton className="size-2" />
          <Skeleton className="h-2.5 w-36" />
          <Skeleton className="h-2.5 w-12" />
          <Skeleton className="h-2.5" style={{ width }} />
        </div>
      ))}
    </div>
  )
}

/** The widths of the placeholder's messages, varied like real ones. */
const SKELETON_MESSAGES = ['38%', '24%', '46%', '30%', '42%', '20%', '34%', '28%', '44%', '26%', '40%', '32%']

function StreamEnd({ end, loading, error, count, onRetry }: {
  end: LogStreamProps['end']; loading: boolean; error: string | null; count: number; onRetry: () => void
}) {
  let content: React.ReactNode
  if (error) {
    content = <>Older Logs didn't load: {error} <button className="underline" onClick={onRetry}>Try again</button></>
  } else if (loading || end === 'more') {
    content = 'Loading older Logs…'
  } else if (end === 'capped') {
    content = `Showing the newest ${count.toLocaleString()} Logs. Narrow the time range to see older Logs.`
  } else {
    content = count === 0 ? '' : 'No older Logs in this time range.'
  }
  return (
    <div role="row" aria-rowindex={count + 2} className="sticky left-0 w-[min(100%,100vw)] px-3 text-center font-sans text-xs leading-[30px] text-muted-foreground">
      <div role="gridcell">{content}</div>
    </div>
  )
}

function readWidths(): Record<string, number> {
  try {
    const saved = JSON.parse(localStorage.getItem(WIDTHS_KEY) ?? '{}')
    return saved && typeof saved === 'object' ? saved : {}
  } catch {
    return {}
  }
}

function saveWidths(widths: Record<string, number>) {
  try {
    localStorage.setItem(WIDTHS_KEY, JSON.stringify(widths))
  } catch {
    // A browser without storage forgets the widths on reload.
  }
}
