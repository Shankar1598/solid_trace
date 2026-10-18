import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { Tooltip as TooltipPrimitive } from '@base-ui/react/tooltip'
import {
  Asterisk,
  ChartColumn,
  ChartLine,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  ChevronsDownUp,
  ChevronsUpDown,
  Columns3,
  Copy,
  Crosshair,
  Download,
  Equal,
  EqualNot,
  Search,
  WrapText,
  X,
} from 'lucide-react'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { TooltipContent } from '@/components/ui/tooltip'
import { useShortcut } from '@/components/useShortcut'
import { fetchLog } from '@/lib/logs/api'
import { canFilter, type Chip, type FilterOp } from '@/lib/logs/builder'
import { inspectorFields, inspectorJson, jsonLines, localTime, utcTime, type InspectorField, type JsonLine } from '@/lib/logs/inspector'
import type { AttributeValue, Log } from '@/lib/logs/stream'
import { cn } from '@/lib/utils'
import { LEVEL_TEXT } from './LogStream'
import { Tag, copy, valueText, type ColumnControl } from './TagMenu'
import { LITERAL_TEXT, MUTED_TEXT, STRING_TEXT } from './valueStyle'

/** What the inspector's lines can add to the Query. */
export interface InspectorActions {
  /** Adds a where filter; null where the Query can't take one, as in SQL mode. */
  filter: ((chip: Chip) => void) | null
  /** Counts the Query's Logs by a Field; null outside the Builder. */
  groupBy: ((field: string) => void) | null
  /** Charts a number's Field: its average over time; null where the Query can't be summarized. */
  chart: ((field: string) => void) | null
  /** Shows the Logs of the Log's Service a minute either side of it, with it highlighted; null in SQL mode. */
  around: ((log: Log) => void) | null
  columns: ColumnControl
}

interface LogInspectorProps {
  /** The Log as the stream has it, shown until EventStore's copy arrives. */
  log: Log
  orgSlug: string
  projectSlug: string
  actions: InspectorActions
  /** Opens the Log above or below this one in the stream; null at its ends. */
  onNewer: (() => void) | null
  onOlder: (() => void) | null
  onClose: () => void
}

type Tab = 'table' | 'json'
const TAB_KEY = 'solid-trace.logs.inspector-tab'

function readTab(): Tab {
  try {
    return localStorage.getItem(TAB_KEY) === 'json' ? 'json' : 'table'
  } catch {
    return 'table'
  }
}

function saveTab(tab: Tab) {
  try {
    localStorage.setItem(TAB_KEY, tab)
  } catch {
    // The tab is only a convenience.
  }
}

/** One tooltip for every button in the inspector, as the tag menu is one menu for every tag. */
const TipHandle = createContext<TooltipPrimitive.Handle<string> | null>(null)

/**
 * One Log, over the right of the stream (it doesn't narrow it): its time and
 * main Fields as tags, its message, then its Fields as a table or its JSON.
 * Hovering a line offers it to the Query: show only, hide, has the Field,
 * count by it, a column, or copy. j and k step through the stream.
 */
export function LogInspector({ log: listed, orgSlug, projectSlug, actions, onNewer, onOlder, onClose }: LogInspectorProps) {
  const [fetched, setFetched] = useState<{ log: Log | null; error: string | null } | null>(null)
  const [tab, setTab] = useState<Tab>(readTab)
  const [tip] = useState(() => TooltipPrimitive.createHandle<string>())
  const log = fetched?.log ?? listed

  useEffect(() => {
    const controller = new AbortController()
    setFetched(null)
    fetchLog({ orgSlug, projectSlug, id: listed.id, time: listed.time, signal: controller.signal })
      .then(found => setFetched({ log: found, error: found ? null : 'This Log is no longer stored.' }))
      .catch((error: Error) => {
        if (!controller.signal.aborted) setFetched({ log: null, error: error.message })
      })
    return () => controller.abort()
  }, [orgSlug, projectSlug, listed.id, listed.time])

  // Esc closes it; k and j step to the newer and older Log.
  useShortcut('close', onClose)
  useShortcut('newer', onNewer)
  useShortcut('older', onOlder)

  const utc = utcTime(log.time)
  const level = log.level ?? ''

  return (
    <TipHandle.Provider value={tip}>
      <aside
        aria-label="Log inspector"
        className="absolute inset-y-0 right-0 z-20 flex w-[540px] max-w-full flex-col border-l bg-background text-xs shadow-2xl"
      >
        <header className="border-b px-3 pt-2.5 pb-2">
          <div className="flex items-start gap-2">
            <div className="min-w-0 flex-1">
              {/* Wraps on a phone rather than cutting the time short. */}
              <h2 className="text-sm">
                <span className="text-muted-foreground">Log</span> <span className="font-mono">{localTime(log.time)}</span>
              </h2>
              <Tag field="time" value={utc} columnable className="font-mono text-[11px] text-muted-foreground">{utc}</Tag>
            </div>
            <div className="flex items-center gap-0.5">
              <IconButton label="Newer Log (k)" disabled={!onNewer} onClick={() => onNewer?.()}><ChevronUp /></IconButton>
              <IconButton label="Older Log (j)" disabled={!onOlder} onClick={() => onOlder?.()}><ChevronDown /></IconButton>
              <IconButton label="Close (Esc)" onClick={onClose}><X /></IconButton>
            </div>
          </div>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {level && (
              <Pill field="level" value={level}>
                <span className={cn('h-2 w-2 rounded-full bg-current', LEVEL_TEXT[level])} />
                <span className={LEVEL_TEXT[level]}>{level}</span>
              </Pill>
            )}
            {log.service && <Pill field="service" value={log.service}>{log.service}</Pill>}
            {log.environment && <Pill field="environment" value={log.environment}>{log.environment}</Pill>}
            {log.trace_id && <Pill field="trace_id" value={log.trace_id}><span className="text-muted-foreground">trace</span> {log.trace_id.slice(0, 8)}</Pill>}
            {actions.around && (
              <button
                type="button"
                onClick={() => actions.around?.(log)}
                className="inline-flex h-6 items-center gap-1.5 border px-2 text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                <Crosshair className="h-3.5 w-3.5" /> Logs around this one
              </button>
            )}
          </div>
        </header>

        <div className="max-h-40 shrink-0 overflow-auto border-b px-3 py-2.5 font-mono text-[13px] break-words whitespace-pre-wrap">
          {log.message}
          {fetched?.error && <p className="mt-2 text-xs text-muted-foreground">{fetched.error} Showing it as the stream loaded it.</p>}
        </div>

        <Tabs value={tab} onValueChange={v => { setTab(v as Tab); saveTab(v as Tab) }} className="min-h-0 flex-1 gap-0">
          <TabsList variant="line" className="px-3">
            <TabsTrigger value="table">Table</TabsTrigger>
            <TabsTrigger value="json">JSON</TabsTrigger>
          </TabsList>
          <TabsContent value="table" className="flex min-h-0 flex-col border-t">
            <FieldTable log={log} actions={actions} />
          </TabsContent>
          <TabsContent value="json" className="flex min-h-0 flex-col border-t">
            <JsonView log={log} actions={actions} />
          </TabsContent>
        </Tabs>
      </aside>
      <TooltipPrimitive.Root handle={tip}>
        {({ payload }) => (payload ? <TooltipContent>{payload}</TooltipContent> : null)}
      </TooltipPrimitive.Root>
    </TipHandle.Provider>
  )
}

/** One of the header's main Fields: a tag, so it opens the tag menu. */
function Pill({ field, value, children }: { field: string; value: string; children: React.ReactNode }) {
  return (
    <Tag field={field} value={value} columnable className="inline-flex h-6 items-center gap-1.5 border px-2 font-mono">
      {children}
    </Tag>
  )
}

/** The Log's Fields, name and value, under Log and Attributes; typing narrows them. */
function FieldTable({ log, actions }: { log: Log; actions: InspectorActions }) {
  const [typed, setTyped] = useState('')
  const t = typed.trim().toLowerCase()
  const fields = inspectorFields(log)
  const shown = t === '' ? fields : fields.filter(f => f.name.toLowerCase().includes(t) || valueText(f.value).toLowerCase().includes(t))
  const attributes = new Set(Object.keys(log.attributes ?? {}))
  const builtIn = shown.filter(f => !attributes.has(f.name))
  const own = shown.filter(f => attributes.has(f.name))

  return (
    <>
      <div className="flex h-8 shrink-0 items-center gap-2 border-b px-3">
        <Search className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <input
          value={typed}
          onChange={e => setTyped(e.target.value)}
          placeholder="Find a Field or value…"
          aria-label="Find a Field or value"
          className="h-full min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
        />
        {t !== '' && <span className="text-[11px] text-muted-foreground">{shown.length} of {fields.length}</span>}
      </div>
      <div className="min-h-0 flex-1 overflow-auto pb-2">
        {shown.length === 0 && <p className="px-3 py-6 text-center text-muted-foreground">No Field or value has "{typed.trim()}".</p>}
        {builtIn.length > 0 && <Section title="Log">{builtIn.map(f => <FieldRow key={f.name} field={f} actions={actions} />)}</Section>}
        {own.length > 0 && <Section title={`Attributes · ${own.length}`}>{own.map(f => <FieldRow key={f.name} field={f} actions={actions} />)}</Section>}
      </div>
    </>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <h3 className="caps-label px-3 pt-3 pb-1">{title}</h3>
      {children}
    </section>
  )
}

function FieldRow({ field: { name, value }, actions }: { field: InspectorField; actions: InspectorActions }) {
  return (
    <div className="group relative flex gap-3 border-b border-border/60 px-3 py-1.5 font-mono hover:bg-muted/40">
      <div className="w-36 shrink-0 truncate text-muted-foreground" title={name}>{name}</div>
      <div className="min-w-0 flex-1 wrap-anywhere whitespace-pre-wrap">
        <ValueText value={value} plainStrings />
      </div>
      <LineActions field={name} value={value} actions={actions} />
    </div>
  )
}

/** A value coloured by type; plainStrings leaves text uncoloured, as a table reads best. */
function ValueText({ value, plainStrings }: { value: AttributeValue; plainStrings?: boolean }) {
  if (typeof value === 'string') return <span className={plainStrings ? undefined : STRING_TEXT}>{plainStrings ? value : JSON.stringify(value)}</span>
  if (typeof value === 'number' || typeof value === 'boolean') return <span className={LITERAL_TEXT}>{String(value)}</span>
  if (value === null) return <span className={cn('italic', MUTED_TEXT)}>null</span>
  return <span className={MUTED_TEXT}>{JSON.stringify(value)}</span>
}

/** The Log's JSON a line each, numbered, its brackets folding. */
function JsonView({ log, actions }: { log: Log; actions: InspectorActions }) {
  const lines = useMemo(() => jsonLines(log), [log])
  const [folded, setFolded] = useState<Set<number>>(() => new Set())
  const [wrap, setWrap] = useState(true)
  const foldable = (i: number) => i > 0 && lines[i].kind === 'open'
  const toggle = (i: number) => setFolded(f => {
    const next = new Set(f)
    if (!next.delete(i)) next.add(i)
    return next
  })

  const rows: React.ReactNode[] = []
  for (let i = 0; i < lines.length; i++) {
    const shut = folded.has(i) && foldable(i)
    rows.push(<JsonRow key={i} index={i} line={lines[i]} lines={lines} shut={shut} foldable={foldable(i)} onFold={() => toggle(i)} wrap={wrap} actions={actions} />)
    if (shut) i = lines[i].end!
  }

  return (
    <>
      <div className="flex h-8 shrink-0 items-center gap-0.5 border-b pr-2 pl-3">
        <span className="flex-1 text-muted-foreground">{'{}'} JSON</span>
        <IconButton label="Unfold all" onClick={() => setFolded(new Set())}><ChevronsUpDown /></IconButton>
        <IconButton label="Fold all" onClick={() => setFolded(new Set(lines.flatMap((_, i) => (foldable(i) ? [i] : []))))}><ChevronsDownUp /></IconButton>
        <IconButton label={wrap ? 'Don\'t wrap lines' : 'Wrap lines'} pressed={wrap} onClick={() => setWrap(w => !w)}><WrapText /></IconButton>
        <IconButton label="Copy the JSON" onClick={() => copy(inspectorJson(log), 'JSON')}><Copy /></IconButton>
        <IconButton label="Download the JSON" onClick={() => download(log)}><Download /></IconButton>
      </div>
      <div className="min-h-0 flex-1 overflow-auto py-1 font-mono">{rows}</div>
    </>
  )
}

function JsonRow({ index, line, lines, shut, foldable, onFold, wrap, actions }: {
  index: number
  line: JsonLine
  lines: JsonLine[]
  shut: boolean
  foldable: boolean
  onFold: () => void
  wrap: boolean
  actions: InspectorActions
}) {
  const comma = shut ? lines[line.end!].comma : line.comma
  return (
    <div className="group relative flex min-w-fit hover:bg-muted/40">
      <span className="w-9 shrink-0 pr-2 text-right text-muted-foreground/50 tabular-nums select-none">{index + 1}</span>
      <span className="w-4 shrink-0">
        {foldable && (
          <button type="button" onClick={onFold} aria-label={shut ? 'Unfold' : 'Fold'} className="flex h-full w-full items-center justify-center text-muted-foreground hover:text-foreground">
            <ChevronRight className={cn('h-3 w-3 transition-transform', !shut && 'rotate-90')} />
          </button>
        )}
      </span>
      <span className={cn('min-w-0 flex-1 pr-2', wrap ? 'wrap-anywhere whitespace-pre-wrap' : 'whitespace-pre')} style={{ paddingLeft: `${line.depth * 2}ch` }}>
        {line.key !== undefined && <>{JSON.stringify(line.key)}<span className={MUTED_TEXT}>: </span></>}
        {shut ? (
          <button type="button" onClick={onFold} className="text-muted-foreground hover:text-foreground">
            {line.text} … {line.size} {line.text === '[' ? (line.size === 1 ? 'item' : 'items') : (line.size === 1 ? 'key' : 'keys')} {line.text === '[' ? ']' : '}'}
          </button>
        ) : (
          <span className={TOKEN_CLASS[line.kind]}>{line.text}</span>
        )}
        {comma && <span className={MUTED_TEXT}>,</span>}
        {line.field === 'time' && <span className="text-muted-foreground/70">{'  // '}{localTime(String(line.value))}</span>}
      </span>
      {line.field !== undefined ? (
        <LineActions field={line.field} value={line.value ?? null} actions={actions} />
      ) : line.elementOf !== undefined ? (
        <ElementActions field={line.elementOf} value={line.value ?? null} actions={actions} />
      ) : line.value !== undefined && line.kind !== 'close' && index > 0 ? (
        <Toolbar><IconButton label="Copy the value" onClick={() => copy(line.value!)}><Copy /></IconButton></Toolbar>
      ) : null}
    </div>
  )
}

const TOKEN_CLASS: Record<JsonLine['kind'], string> = {
  string: STRING_TEXT,
  number: LITERAL_TEXT,
  bool: LITERAL_TEXT,
  null: cn('italic', MUTED_TEXT),
  empty: MUTED_TEXT,
  open: MUTED_TEXT,
  close: MUTED_TEXT,
}

/** Why a Field's value can't be added to the Query as a filter, or null when it can. */
function filterBlock(field: string, actions: InspectorActions): string | null {
  if (field === 'time') return 'The time range filters on time.'
  if (!actions.filter) return 'SQL takes no filters from here.'
  return null
}

/** A line's buttons that add its Field to the Query, shown while it's hovered. */
function LineActions({ field, value, actions }: { field: string; value: AttributeValue; actions: InspectorActions }) {
  const blocked = filterBlock(field, actions)
  const scalar = canFilter(value)
  const shown = actions.columns.shown.includes(field)
  const filter = (op: FilterOp) => actions.filter?.({ field, op, value: op === 'exists' ? null : (value as Chip['value']) })
  const as = scalar ? `${field} == ${JSON.stringify(value)}` : ''
  const groupLabel = field === 'time' ? 'Group by time with Summarize\'s time bins.' : actions.groupBy ? `Count Logs by ${field}` : 'Counting by a Field is in the Builder.'

  return (
    <Toolbar>
      <span className="flex">
        <IconButton label={blocked ?? (scalar ? `Show only ${as}` : 'Only text, numbers and bools can be matched.')} disabled={!!blocked || !scalar} onClick={() => filter('==')}><Equal /></IconButton>
        <IconButton label={blocked ?? (scalar ? `Hide ${as}` : 'Only text, numbers and bools can be matched.')} disabled={!!blocked || !scalar} onClick={() => filter('!=')}><EqualNot /></IconButton>
        <IconButton label={blocked ?? `Show only Logs with ${field}`} disabled={!!blocked} onClick={() => filter('exists')}><Asterisk /></IconButton>
      </span>
      <span className="flex border-l">
        <IconButton label={groupLabel} disabled={!actions.groupBy || field === 'time'} onClick={() => actions.groupBy?.(field)}><ChartColumn /></IconButton>
        {typeof value === 'number' && (
          <IconButton
            label={actions.chart ? `Chart this field: avg(${field}) over time` : 'SQL charts nothing from here.'}
            disabled={!actions.chart}
            onClick={() => actions.chart?.(field)}
          >
            <ChartLine />
          </IconButton>
        )}
        <IconButton label={shown ? 'Remove the column' : 'Add as a column'} pressed={shown} disabled={shown && actions.columns.shown.length === 1} onClick={() => actions.columns.toggle(field)}><Columns3 /></IconButton>
        <IconButton label="Copy the value" onClick={() => copy(value)}><Copy /></IconButton>
      </span>
    </Toolbar>
  )
}

/** An array Field's element: Logs whose Field contains it, or doesn't. */
function ElementActions({ field, value, actions }: { field: string; value: AttributeValue; actions: InspectorActions }) {
  const blocked = filterBlock(field, actions)
  const scalar = canFilter(value)
  const text = scalar ? String(value) : ''
  const filter = (op: FilterOp) => actions.filter?.({ field, op, value: text })
  return (
    <Toolbar>
      <span className="flex">
        <IconButton label={blocked ?? (scalar ? `Show only ${field} contains ${JSON.stringify(text)}` : 'Only text, numbers and bools can be matched.')} disabled={!!blocked || !scalar} onClick={() => filter('contains')}><Equal /></IconButton>
        <IconButton label={blocked ?? (scalar ? `Hide ${field} contains ${JSON.stringify(text)}` : 'Only text, numbers and bools can be matched.')} disabled={!!blocked || !scalar} onClick={() => filter('!contains')}><EqualNot /></IconButton>
      </span>
      <span className="flex border-l">
        <IconButton label="Copy the value" onClick={() => copy(value)}><Copy /></IconButton>
      </span>
    </Toolbar>
  )
}

function Toolbar({ children }: { children: React.ReactNode }) {
  return (
    <span className="absolute top-0.5 right-1 z-10 hidden items-center border bg-popover shadow-sm group-focus-within:flex group-hover:flex">
      {children}
    </span>
  )
}

/** A small icon button whose label shows in the inspector's tooltip; disabled ones still say why. */
function IconButton({ label, onClick, disabled, pressed, children }: {
  label: string
  onClick: () => void
  disabled?: boolean
  pressed?: boolean
  children: React.ReactNode
}) {
  const tip = useContext(TipHandle)
  return (
    <TooltipPrimitive.Trigger
      handle={tip ?? undefined}
      payload={label}
      delay={150}
      render={(
        <button
          type="button"
          aria-label={label}
          aria-disabled={disabled || undefined}
          aria-pressed={pressed}
          onClick={e => {
            e.stopPropagation()
            if (!disabled) onClick()
          }}
          className={cn(
            'flex h-6 w-6 items-center justify-center text-muted-foreground [&_svg]:h-3.5 [&_svg]:w-3.5',
            disabled ? 'cursor-default opacity-40' : 'hover:bg-muted hover:text-foreground',
            pressed && 'bg-accent text-accent-foreground',
          )}
        />
      )}
    >
      {children}
    </TooltipPrimitive.Trigger>
  )
}

function download(log: Log) {
  const url = URL.createObjectURL(new Blob([inspectorJson(log)], { type: 'application/json' }))
  const link = document.createElement('a')
  link.href = url
  link.download = `log-${log.id}.json`
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
