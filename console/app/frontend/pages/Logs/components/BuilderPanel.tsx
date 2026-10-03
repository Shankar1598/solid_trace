import { createContext, useContext, useState } from 'react'
import { Popover } from '@base-ui/react/popover'
import { Plus, X } from 'lucide-react'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  aggregateText,
  editableText,
  groupText,
  parseValue,
  setFilter,
  setSort,
  setSummarize,
  setTake,
  summarizeColumns,
  type Aggregate,
  type AggregateFn,
  type Builder,
  type Chip,
  type ChipValue,
  type FilterOp,
  type Group,
  type SortKey,
  type Summarize,
} from '@/lib/logs/builder'
import type { FieldInfo } from '@/lib/logs/columns'
import { cn } from '@/lib/utils'
import { SuggestInput, type Suggestion } from './SuggestInput'
import { valueText } from './TagMenu'

export interface ProjectOption {
  id: number
  name: string
  slug: string
}

interface BuilderPanelProps {
  builder: Builder
  /** The Project whose Logs the Query reads, and those it can read instead. */
  project: ProjectOption
  projects: ProjectOption[]
  onProject: (slug: string) => void
  /** Fields to suggest: the built-in fields and the Attributes in view, with their types. */
  fields: FieldInfo[]
  /** A Field's common values in view, to suggest. */
  valuesOf: (field: string) => ChipValue[]
  /** A changed Builder, which runs at once. */
  onChange: (builder: Builder) => void
}

const EMPTY_SUMMARIZE: Summarize = { aggregates: [], by: [] }

/** What the editors suggest. */
interface Suggestions {
  fields: FieldInfo[]
  valuesOf: (field: string) => ChipValue[]
}
const SuggestionsContext = createContext<Suggestions>({ fields: [], valuesOf: () => [] })

/** An input suggesting Fields, those of a type in `types` first when given. */
function FieldInput({ value, onChange, placeholder, autoFocus, types }: {
  value: string; onChange: (v: string) => void; placeholder: string; autoFocus?: boolean; types?: string[]
}) {
  const { fields } = useContext(SuggestionsContext)
  const fitting = types ? [...fields.filter(f => types.includes(f.type)), ...fields.filter(f => !types.includes(f.type))] : fields
  const suggestions: Suggestion[] = fitting.filter(f => f.name !== 'time').map(f => ({ value: f.name, hint: f.type }))
  return <SuggestInput value={value} onChange={onChange} suggestions={suggestions} placeholder={placeholder} autoFocus={autoFocus} className={INPUT} />
}

/**
 * The Builder's clauses, one row each: the Project, Where filters, Summarize
 * functions by Fields and time bins, and More (sort and limit). Each + adds a
 * clause and clicking one edits it; every change runs the Query.
 */
export function BuilderPanel({ builder, project, projects, onProject, fields, valuesOf, onChange }: BuilderPanelProps) {
  const summarize = builder.summarize ?? EMPTY_SUMMARIZE
  const changeSummarize = (next: Summarize) => onChange(setSummarize(builder, next))
  const sortColumns = builder.summarize ? summarizeColumns(builder.summarize) : null

  return (
    <SuggestionsContext.Provider value={{ fields, valuesOf }}>
    <div className="border-b px-3 py-2 font-mono text-xs">
      <div className="ml-1 border-l pl-4">
        <Row label="Project">
          <ProjectSelect project={project} projects={projects} onChange={onProject} />
        </Row>

        <Row label="Where">
          {builder.filters.map((chip, i) => (
            <Clause
              key={i}
              text={filterLabel(chip)}
              onRemove={() => onChange({ ...builder, filters: builder.filters.filter((_, j) => j !== i) })}
              editor={close => <FilterEditor chip={chip} onApply={c => { close(); onChange(setFilter(builder, i, c)) }} />}
            />
          ))}
          <Add label="Add a filter" editor={close => (
            <FilterEditor onApply={c => { close(); onChange(setFilter(builder, builder.filters.length, c)) }} />
          )} />
        </Row>

        <Row label="Summarize">
          {summarize.aggregates.map((a, i) => (
            <Clause
              key={i}
              text={aggregateText(a)}
              onRemove={() => changeSummarize({ ...summarize, aggregates: summarize.aggregates.filter((_, j) => j !== i) })}
              editor={close => <AggregateEditor aggregate={a} onApply={next => {
                close()
                changeSummarize({ ...summarize, aggregates: summarize.aggregates.map((old, j) => (j === i ? next : old)) })
              }} />}
            />
          ))}
          <Add label="Add a function" editor={close => (
            <AggregateEditor onApply={a => { close(); changeSummarize({ ...summarize, aggregates: [...summarize.aggregates, a] }) }} />
          )} />
          <span className="ml-3 text-muted-foreground">by</span>
          {summarize.by.map((g, i) => (
            <Clause
              key={i}
              text={groupText(g)}
              onRemove={() => changeSummarize({ ...summarize, by: summarize.by.filter((_, j) => j !== i) })}
              editor={close => <GroupEditor group={g} onApply={next => {
                close()
                changeSummarize({ ...summarize, by: summarize.by.map((old, j) => (j === i ? next : old)) })
              }} />}
            />
          ))}
          <Add label="Group by a Field or time" editor={close => (
            <GroupEditor onApply={g => { close(); changeSummarize({ ...summarize, by: [...summarize.by, g] }) }} />
          )} />
        </Row>

        <Row label="More" muted={!builder.sort && builder.take === undefined}>
          {(builder.sort ?? []).map((key, i) => (
            <Clause
              key={i}
              text={`sort by ${key.column} ${key.asc ? 'asc' : 'desc'}`}
              onRemove={() => onChange(setSort(builder, (builder.sort ?? []).filter((_, j) => j !== i)))}
              editor={close => <SortEditor columns={sortColumns} sortKey={key} onApply={next => {
                close()
                onChange(setSort(builder, (builder.sort ?? []).map((old, j) => (j === i ? next : old))))
              }} />}
            />
          ))}
          {builder.take !== undefined && (
            <Clause
              text={`limit ${builder.take}`}
              onRemove={() => onChange(setTake(builder, null))}
              editor={close => <LimitEditor take={builder.take} onApply={n => { close(); onChange(setTake(builder, n)) }} />}
            />
          )}
          <Add label="Sort or limit" editor={close => (
            <MoreEditor
              columns={sortColumns}
              hasLimit={builder.take !== undefined}
              onSort={key => { close(); onChange(setSort(builder, [...(builder.sort ?? []), key])) }}
              onLimit={n => { close(); onChange(setTake(builder, n)) }}
            />
          )} />
        </Row>
      </div>
    </div>
    </SuggestionsContext.Provider>
  )
}

/** The Project by its slug, as the Query names it. */
function ProjectSelect({ project, projects, onChange }: { project: ProjectOption; projects: ProjectOption[]; onChange: (slug: string) => void }) {
  const items = Object.fromEntries(projects.map(p => [p.slug, p.slug]))
  return (
    <Select value={project.slug} onValueChange={v => v && onChange(v as string)} items={items}>
      <SelectTrigger className="h-7 w-fit font-mono text-xs" aria-label="Project"><SelectValue /></SelectTrigger>
      <SelectContent>
        {projects.map(p => (
          <SelectItem key={p.slug} value={p.slug}>
            <span className="font-mono">{p.slug}</span>
            {p.name !== p.slug && <span className="text-muted-foreground">{p.name}</span>}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function Row({ label, muted, children }: { label: string; muted?: boolean; children: React.ReactNode }) {
  return (
    <div className="relative flex min-h-8 flex-wrap items-center gap-1.5 py-0.5">
      <span className="absolute -left-4 top-1/2 h-px w-2.5 bg-border" />
      <span className={cn('w-24 shrink-0', muted ? 'text-muted-foreground' : 'text-foreground')}>{label}</span>
      {children}
    </div>
  )
}

/** A clause: click it to edit it in a popover, × to remove it. */
function Clause({ text, editor, onRemove }: { text: string; editor: (close: () => void) => React.ReactNode; onRemove: () => void }) {
  const [open, setOpen] = useState(false)
  return (
    <span className="inline-flex max-w-96 items-center rounded border bg-muted/50">
      <Popover.Root open={open} onOpenChange={setOpen}>
        <Popover.Trigger className="truncate py-1 pl-2 pr-1 text-left hover:text-primary" title={text}>{text}</Popover.Trigger>
        <EditorPopup>{editor(() => setOpen(false))}</EditorPopup>
      </Popover.Root>
      <button className="px-1.5 py-1 text-muted-foreground hover:bg-muted hover:text-foreground" onClick={onRemove} aria-label={`Remove ${text}`}>
        <X className="h-3 w-3" />
      </button>
    </span>
  )
}

function Add({ label, editor }: { label: string; editor: (close: () => void) => React.ReactNode }) {
  const [open, setOpen] = useState(false)
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger className="flex h-6 w-6 items-center justify-center rounded border hover:bg-muted" aria-label={label} title={label}>
        <Plus className="h-3.5 w-3.5" />
      </Popover.Trigger>
      <EditorPopup>{editor(() => setOpen(false))}</EditorPopup>
    </Popover.Root>
  )
}

function EditorPopup({ children }: { children: React.ReactNode }) {
  return (
    <Popover.Portal>
      <Popover.Positioner className="isolate z-50" side="bottom" align="start" sideOffset={4}>
        <Popover.Popup className="w-80 bg-popover p-3 font-sans text-xs text-popover-foreground shadow-md ring-1 ring-foreground/10 outline-none">
          {children}
        </Popover.Popup>
      </Popover.Positioner>
    </Popover.Portal>
  )
}

const OPS: { op: FilterOp; label: string }[] = [
  { op: '==', label: '==  equals' },
  { op: '!=', label: '!=  doesn\'t equal' },
  { op: '>', label: '>' },
  { op: '>=', label: '>=' },
  { op: '<', label: '<' },
  { op: '<=', label: '<=' },
  { op: 'contains', label: 'contains' },
  { op: '!contains', label: 'doesn\'t contain' },
  { op: 'startswith', label: 'starts with' },
  { op: '!startswith', label: 'doesn\'t start with' },
  { op: 'in', label: 'in  (one of)' },
  { op: '!in', label: '!in  (none of)' },
  { op: 'exists', label: 'exists' },
  { op: 'missing', label: 'is missing' },
]

function filterLabel(chip: Chip): string {
  switch (chip.op) {
    case 'exists':
      return `${chip.field} exists`
    case 'missing':
      return `${chip.field} is missing`
    case 'in':
    case '!in':
      return `${chip.field} ${chip.op} (${(Array.isArray(chip.value) ? chip.value : []).map(v => valueText(v)).join(', ')})`
    default:
      return `${chip.field} ${chip.op} ${typeof chip.value === 'string' ? `"${chip.value}"` : valueText(chip.value as ChipValue)}`
  }
}

/**
 * A Field's common values as suggestions for the value box. For a list, the
 * suggestion is the list so far with the value added.
 */
function valueSuggestions(values: ChipValue[], typed: string, list: boolean, text: boolean): Suggestion[] {
  const asText = (v: ChipValue) => (text ? String(v) : editableText(v))
  if (!list) return values.map(v => ({ value: asText(v), hint: typeof v === 'boolean' ? 'bool' : typeof v }))
  const parts = typed.split(',').map(p => p.trim())
  const done = parts.slice(0, -1).filter(p => p !== '')
  return values
    .map(asText)
    .filter(v => !done.includes(v))
    .map(v => ({ value: [...done, v].join(', '), hint: undefined }))
}

/** The text a filter's value is edited as: a list comma-separated. */
function filterValueText(chip: Chip | undefined): string {
  if (!chip || chip.value === null) return ''
  if (Array.isArray(chip.value)) return chip.value.map(editableText).join(', ')
  return chip.op.includes('contains') || chip.op.includes('startswith') ? String(chip.value) : editableText(chip.value)
}

function FilterEditor({ chip, onApply }: { chip?: Chip; onApply: (chip: Chip) => void }) {
  const [field, setField] = useState(chip?.field ?? '')
  const [op, setOp] = useState<FilterOp>(chip?.op ?? '==')
  const [value, setValue] = useState(filterValueText(chip))
  const { fields, valuesOf } = useContext(SuggestionsContext)
  const needsValue = op !== 'exists' && op !== 'missing'
  const fieldType = fields.find(f => f.name === field.trim())?.type
  const text = op.includes('contains') || op.includes('startswith')
  const list = op === 'in' || op === '!in'

  const chipOf = (): Chip | null => {
    const name = field.trim()
    if (name === '' || name === 'time' || name === '_time') return null
    if (!needsValue) return { field: name, op, value: null }
    if (list) {
      const values = value.split(',').map(v => v.trim()).filter(v => v !== '').map(parseValue)
      return values.length > 0 ? { field: name, op, value: values } : null
    }
    if (text) return value === '' ? null : { field: name, op, value }
    if (value.trim() === '') return null
    return { field: name, op, value: parseValue(value) }
  }
  const next = chipOf()

  return (
    <EditorForm onSubmit={() => next && onApply(next)} disabled={!next}>
      <Labelled label="Field">
        <FieldInput autoFocus value={field} onChange={setField} placeholder="service" />
      </Labelled>
      <Labelled label="Operator">
        <select value={op} onChange={e => setOp(e.target.value as FilterOp)} className={INPUT}>
          {OPS.map(o => <option key={o.op} value={o.op}>{o.label}</option>)}
        </select>
      </Labelled>
      {needsValue && (
        <Labelled label={list ? 'Values, comma-separated' : 'Value'}>
          <SuggestInput
            value={value}
            onChange={setValue}
            suggestions={valueSuggestions(valuesOf(field.trim()), value, list, text)}
            placeholder={list ? '500, 502, 503' : text ? 'text' : 'api, 500 or true'}
            className={INPUT}
          />
        </Labelled>
      )}
      {needsValue && !text && (
        <p className="text-muted-foreground">
          {fieldType && fieldType !== 'mixed' ? `${field.trim()} holds ${fieldType === 'array' ? 'arrays' : `${fieldType}s`} in the Logs shown. ` : ''}
          Numbers and true/false are matched as such. Quote a value, as "500", to match it as text.
        </p>
      )}
      {(field.trim() === 'time' || field.trim() === '_time') && <p className="text-muted-foreground">Use the time range to filter on time.</p>}
    </EditorForm>
  )
}

const FNS: { fn: AggregateFn; label: string }[] = [
  { fn: 'count', label: 'count — Logs' },
  { fn: 'dcount', label: 'dcount — distinct values' },
  { fn: 'sum', label: 'sum' },
  { fn: 'avg', label: 'avg' },
  { fn: 'min', label: 'min' },
  { fn: 'max', label: 'max' },
  { fn: 'percentile', label: 'percentile' },
]

function AggregateEditor({ aggregate, onApply }: { aggregate?: Aggregate; onApply: (a: Aggregate) => void }) {
  const [fn, setFn] = useState<AggregateFn>(aggregate?.fn ?? 'count')
  const [field, setField] = useState(aggregate?.field ?? '')
  const [percent, setPercent] = useState(String(aggregate?.percent ?? 95))
  const p = Number(percent)
  const next: Aggregate | null = fn === 'count'
    ? { fn }
    : field.trim() === '' || (fn === 'percentile' && !(percent.trim() !== '' && p >= 0 && p <= 100))
      ? null
      : fn === 'percentile' ? { fn, field: field.trim(), percent: p } : { fn, field: field.trim() }

  return (
    <EditorForm onSubmit={() => next && onApply(next)} disabled={!next}>
      <Labelled label="Function">
        <select autoFocus value={fn} onChange={e => setFn(e.target.value as AggregateFn)} className={INPUT}>
          {FNS.map(f => <option key={f.fn} value={f.fn}>{f.label}</option>)}
        </select>
      </Labelled>
      {fn !== 'count' && (
        <Labelled label="Of the Field">
          <FieldInput value={field} onChange={setField} placeholder="duration_ms" types={fn === 'dcount' ? undefined : ['number']} />
        </Labelled>
      )}
      {fn === 'percentile' && (
        <Labelled label="Percentage">
          <select value={percent} onChange={e => setPercent(e.target.value)} className={INPUT}>
            {['50', '75', '90', '95', '99', '99.9'].map(v => <option key={v} value={v}>p{v}</option>)}
          </select>
        </Labelled>
      )}
      {fn !== 'count' && fn !== 'dcount' && <p className="text-muted-foreground">{fn} reads the Field as a number.</p>}
    </EditorForm>
  )
}

const BINS = ['auto', '1m', '5m', '15m', '1h', '6h', '1d']

function GroupEditor({ group, onApply }: { group?: Group; onApply: (g: Group) => void }) {
  const [kind, setKind] = useState<'field' | 'time'>(group?.bin ? 'time' : 'field')
  const [field, setField] = useState(group && !group.bin ? group.field : '')
  const [bin, setBin] = useState(group?.bin ?? 'auto')
  const name = field.trim()
  const next: Group | null = kind === 'time' ? { field: 'time', bin } : name === '' || name === 'time' ? null : { field: name }

  return (
    <EditorForm onSubmit={() => next && onApply(next)} disabled={!next}>
      <div className="flex gap-1">
        {(['field', 'time'] as const).map(k => (
          <button key={k} type="button" onClick={() => setKind(k)}
            className={cn('flex-1 rounded border px-2 py-1', kind === k ? 'bg-primary text-primary-foreground' : 'hover:bg-muted')}>
            {k === 'field' ? 'A Field' : 'Time bins'}
          </button>
        ))}
      </div>
      {kind === 'field' ? (
        <Labelled label="Field">
          <FieldInput autoFocus value={field} onChange={setField} placeholder="service" types={['string', 'bool']} />
        </Labelled>
      ) : (
        <Labelled label="Bin size">
          <select value={bin} onChange={e => setBin(e.target.value)} className={INPUT}>
            {BINS.map(b => <option key={b} value={b}>{b === 'auto' ? 'auto (follows the range)' : b}</option>)}
          </select>
        </Labelled>
      )}
    </EditorForm>
  )
}

/** columns: what a Summarize makes, or null to sort Logs by any Field. */
function SortEditor({ columns, sortKey, onApply }: {
  columns: string[] | null; sortKey?: SortKey; onApply: (key: SortKey) => void
}) {
  const [column, setColumn] = useState(sortKey?.column ?? columns?.[columns.length - 1] ?? '')
  const [asc, setAsc] = useState(sortKey?.asc ?? false)
  const name = column.trim()

  return (
    <EditorForm onSubmit={() => name && onApply({ column: name, asc })} disabled={!name}>
      <Labelled label="Sort by">
        {columns ? (
          <select autoFocus value={column} onChange={e => setColumn(e.target.value)} className={INPUT}>
            {columns.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
        ) : (
          <FieldInput autoFocus value={column} onChange={setColumn} placeholder="duration_ms" />
        )}
      </Labelled>
      <Labelled label="Order">
        <select value={asc ? 'asc' : 'desc'} onChange={e => setAsc(e.target.value === 'asc')} className={INPUT}>
          <option value="desc">descending</option>
          <option value="asc">ascending</option>
        </select>
      </Labelled>
      {!columns && <p className="text-muted-foreground">Sorting Logs by a Field other than time shows them as a table.</p>}
    </EditorForm>
  )
}

function LimitEditor({ take, onApply }: { take?: number; onApply: (n: number) => void }) {
  const [text, setText] = useState(String(take ?? 100))
  const n = Number(text)
  const ok = text.trim() !== '' && Number.isInteger(n) && n >= 0
  return (
    <EditorForm onSubmit={() => ok && onApply(n)} disabled={!ok}>
      <Labelled label="Keep at most">
        <input autoFocus type="number" min={0} value={text} onChange={e => setText(e.target.value)} className={INPUT} />
      </Labelled>
    </EditorForm>
  )
}

function MoreEditor({ columns, hasLimit, onSort, onLimit }: {
  columns: string[] | null; hasLimit: boolean; onSort: (key: SortKey) => void; onLimit: (n: number) => void
}) {
  const [kind, setKind] = useState<'sort' | 'limit'>('sort')
  return (
    <div className="flex flex-col gap-2">
      <div className="flex gap-1">
        {(['sort', 'limit'] as const).map(k => (
          <button key={k} type="button" onClick={() => setKind(k)} disabled={k === 'limit' && hasLimit}
            className={cn('flex-1 rounded border px-2 py-1 disabled:opacity-50', kind === k ? 'bg-primary text-primary-foreground' : 'hover:bg-muted')}>
            {k === 'sort' ? 'Sort' : 'Limit'}
          </button>
        ))}
      </div>
      {kind === 'sort' ? <SortEditor columns={columns} onApply={onSort} /> : <LimitEditor onApply={onLimit} />}
    </div>
  )
}

const INPUT = 'h-7 w-full rounded border bg-background px-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring'

function EditorForm({ onSubmit, disabled, children }: { onSubmit: () => void; disabled: boolean; children: React.ReactNode }) {
  return (
    <form className="flex flex-col gap-2" onSubmit={e => { e.preventDefault(); onSubmit() }}>
      {children}
      <button type="submit" disabled={disabled} className="h-7 self-end rounded bg-primary px-3 text-primary-foreground disabled:opacity-50">
        Apply
      </button>
    </form>
  )
}

function Labelled({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-muted-foreground">{label}</span>
      {children}
    </label>
  )
}
