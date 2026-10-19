import { Fragment, createContext, useContext, useState } from 'react'
import { Popover } from '@base-ui/react/popover'
import { ChevronDown, ChevronLeft, Plus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useShortcut } from '@/components/useShortcut'
import {
  addAggregate,
  aggregateText,
  editableText,
  groupText,
  parseValue,
  removeAggregate,
  search,
  setFilter,
  setSort,
  setSummarize,
  setTake,
  summarizeColumns,
  WITHOUT_FIELD,
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
import { fieldOptions, listFragment, listParts, narrow, toggleListValue, valueOptions, type PickOption } from '@/lib/logs/pick'
import type { ShortcutId } from '@/lib/shortcuts'
import { cn } from '@/lib/utils'
import { MenuList, OptionList, PickList, useHighlight } from './PickList'
import { valueText } from './TagMenu'
import { LITERAL_TEXT, MUTED_TEXT, STRING_TEXT } from './valueStyle'

export interface ProjectOption {
  id: number
  name: string
  slug: string
  /** How many days back the Project's Logs go: its Retention. */
  log_retention_days: number
}

interface BuilderPanelProps {
  builder: Builder
  /** The Project whose Logs the Query reads, and those it can read instead. */
  project: ProjectOption
  projects: ProjectOption[]
  onProject: (slug: string) => void
  /** Fields to suggest: the built-in fields and the Attributes of recent Logs, with their types. */
  fields: FieldInfo[]
  /** A Field's common values in recent Logs, to suggest. */
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

/** Offered for what's typed into a Field list, when no Field seen is just that. */
const TYPED_FIELD = { hint: 'not seen' }

/**
 * The Builder's clauses, one row each: the Project, Where filters, Summarize
 * functions by Fields and time bins, then the search, sort and limit, each
 * row there once More has added it. Each + adds a clause in steps (pick a
 * Field, then say how) and clicking one edits it; every change runs the Query.
 */
export function BuilderPanel({ builder, project, projects, onProject, fields, valuesOf, onChange }: BuilderPanelProps) {
  const summarize = builder.summarize ?? EMPTY_SUMMARIZE
  const changeSummarize = (next: Summarize) => onChange(setSummarize(builder, next))
  const sortColumns = builder.summarize ? summarizeColumns(builder.summarize) : null
  // What More can still add: once added, each has a row of its own.
  const more = MORE.filter(k => (k === 'search' ? builder.search === '' : k === 'sort' ? !builder.sort : builder.take === undefined))

  return (
    <SuggestionsContext.Provider value={{ fields, valuesOf }}>
    <div className="px-3 py-2 text-xs">
      <div className="ml-1 border-l pl-4">
        <Row label="Project">
          <ProjectSelect project={project} projects={projects} onChange={onProject} />
        </Row>

        <Row label="Where" muted={builder.filters.length === 0}>
          {builder.filters.map((chip, i) => (
            <Clause
              key={i}
              title={filterLabel(chip)}
              onRemove={() => onChange({ ...builder, filters: builder.filters.filter((_, j) => j !== i) })}
              editor={close => <FilterEditor chip={chip} onApply={c => { close(); onChange(setFilter(builder, i, c)) }} />}
            >
              <FilterText chip={chip} />
            </Clause>
          ))}
          <Add label="Add a filter" shortcut="focus-query" editor={close => (
            <FilterEditor onApply={c => { close(); onChange(setFilter(builder, builder.filters.length, c)) }} />
          )} />
        </Row>

        <Row label="Summarize" muted={!builder.summarize}>
          {summarize.aggregates.map((a, i) => (
            <Clause
              key={i}
              title={aggregateText(a)}
              onRemove={() => onChange(removeAggregate(builder, i))}
              editor={close => <AggregateEditor aggregate={a} onApply={next => {
                close()
                changeSummarize({ ...summarize, aggregates: summarize.aggregates.map((old, j) => (j === i ? next : old)) })
              }} />}
            >
              {aggregateText(a)}
            </Clause>
          ))}
          <Add label="Add a function" editor={close => (
            <AggregateEditor onApply={a => { close(); onChange(addAggregate(builder, a)) }} />
          )} />
          {/* by and its groups wrap as one, so on a phone by leads the groups rather than trailing the functions. */}
          <span className="inline-flex min-w-0 max-w-full flex-wrap items-center gap-1.5">
            <span className={cn('ml-2 mr-0.5', MUTED_TEXT)}>by</span>
            {summarize.by.map((g, i) => (
              <Clause
                key={i}
                title={groupText(g)}
                onRemove={() => changeSummarize({ ...summarize, by: summarize.by.filter((_, j) => j !== i) })}
                editor={close => <GroupEditor group={g} onApply={next => {
                  close()
                  changeSummarize({ ...summarize, by: summarize.by.map((old, j) => (j === i ? next : old)) })
                }} />}
              >
                {groupText(g)}
              </Clause>
            ))}
            <Add label="Group by a Field or time" editor={close => (
              <GroupEditor onApply={g => { close(); changeSummarize({ ...summarize, by: [...summarize.by, g] }) }} />
            )} />
          </span>
        </Row>

        {builder.search !== '' && (
          <Row label="Search">
            <Clause
              title={`search "${builder.search}"`}
              onRemove={() => onChange(search(builder, ''))}
              editor={close => <SearchStep text={builder.search} onApply={text => { close(); onChange(search(builder, text)) }} />}
            >
              <span className={STRING_TEXT}>"{builder.search}"</span>
            </Clause>
          </Row>
        )}

        {builder.sort && (
          <Row label="Sort by">
            {builder.sort.map((key, i) => (
              <Clause
                key={i}
                title={`sort by ${key.column} ${key.asc ? 'asc' : 'desc'}`}
                onRemove={() => onChange(setSort(builder, (builder.sort ?? []).filter((_, j) => j !== i)))}
                editor={close => <SortStep columns={sortColumns} sortKey={key} onApply={next => {
                  close()
                  onChange(setSort(builder, (builder.sort ?? []).map((old, j) => (j === i ? next : old))))
                }} />}
              >
                {key.column} <span className={MUTED_TEXT}>{key.asc ? 'asc' : 'desc'}</span>
              </Clause>
            ))}
            <Add label="Then sort by" editor={close => (
              <SortStep columns={sortColumns} onApply={key => { close(); onChange(setSort(builder, [...(builder.sort ?? []), key])) }} />
            )} />
          </Row>
        )}

        {builder.take !== undefined && (
          <Row label="Limit">
            <Clause
              title={`limit ${builder.take}`}
              onRemove={() => onChange(setTake(builder, null))}
              editor={close => <LimitStep take={builder.take} onApply={n => { close(); onChange(setTake(builder, n)) }} />}
            >
              <span className={LITERAL_TEXT}>{builder.take}</span>
            </Clause>
          </Row>
        )}

        {more.length > 0 && (
          <Row label="More" muted>
            <Add label={`Add a ${listText(more)}`} editor={close => (
              <MoreEditor
                kinds={more}
                columns={sortColumns}
                onSearch={text => { close(); onChange(search(builder, text)) }}
                onSort={key => { close(); onChange(setSort(builder, [key])) }}
                onLimit={n => { close(); onChange(setTake(builder, n)) }}
              />
            )} />
          </Row>
        )}
      </div>
    </div>
    </SuggestionsContext.Provider>
  )
}

/** The Project by its slug, as the Query names it; the list shows each one's name too. */
function ProjectSelect({ project, projects, onChange }: { project: ProjectOption; projects: ProjectOption[]; onChange: (slug: string) => void }) {
  const items = Object.fromEntries(projects.map(p => [p.slug, p.slug]))
  return (
    <Select value={project.slug} onValueChange={v => v && onChange(v as string)} items={items}>
      <SelectTrigger size="sm" className="h-6 w-fit max-w-full font-mono" aria-label="Project"><SelectValue /></SelectTrigger>
      <SelectContent align="start" alignItemWithTrigger={false} className="w-auto min-w-(--anchor-width) max-w-(--available-width)">
        {projects.map(p => (
          <SelectItem key={p.slug} value={p.slug}>
            <span>{p.slug}</span>
            {p.name !== p.slug && <span className="ml-auto pl-4 text-muted-foreground">{p.name}</span>}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

/** A clause row: its label, then its clauses, which wrap under one another rather than under the label. */
function Row({ label, muted, children }: { label: string; muted?: boolean; children: React.ReactNode }) {
  return (
    <div className="relative flex min-h-8 items-start gap-1.5 py-0.5">
      <span className="absolute -left-4 top-4 h-px w-2.5 bg-border" />
      <span className={cn('w-24 shrink-0 leading-7', muted ? 'text-muted-foreground' : 'text-foreground')}>{label}</span>
      <div className="flex min-h-7 min-w-0 flex-1 flex-wrap items-center gap-1.5">{children}</div>
    </div>
  )
}


/** A clause: click it to edit it in a popover, × to remove it. */
function Clause({ title, editor, onRemove, children }: {
  title: string
  editor: (close: () => void) => React.ReactNode
  onRemove: () => void
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  return (
    // At most the row's width, so a long clause truncates on a phone rather than pushing its × out of reach.
    <span className={cn('inline-flex h-6 max-w-[min(24rem,100%)] items-stretch border bg-muted/40 font-mono', open && 'border-ring')}>
      <Popover.Root open={open} onOpenChange={setOpen}>
        <Popover.Trigger className="min-w-0 truncate px-2 text-left hover:bg-muted" title={`${title} (click to edit)`}>
          {children}
        </Popover.Trigger>
        <EditorPopup>{editor(() => setOpen(false))}</EditorPopup>
      </Popover.Root>
      <button
        type="button"
        className="flex items-center border-l px-1.5 text-muted-foreground hover:bg-muted hover:text-foreground"
        onClick={onRemove}
        aria-label={`Remove ${title}`}
        title="Remove"
      >
        <X className="h-3 w-3" />
      </button>
    </span>
  )
}

/** A + that opens a clause's editor; `shortcut` opens it from the keyboard too. */
function Add({ label, shortcut, editor }: { label: string; shortcut?: ShortcutId; editor: (close: () => void) => React.ReactNode }) {
  const [open, setOpen] = useState(false)
  useShortcut(shortcut ?? 'focus-query', shortcut ? () => setOpen(true) : null)
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger
        className="flex h-6 w-6 items-center justify-center border text-muted-foreground hover:bg-muted hover:text-foreground data-[popup-open]:border-ring data-[popup-open]:bg-muted data-[popup-open]:text-foreground"
        aria-label={label}
        title={label}
      >
        <Plus className="h-3.5 w-3.5" />
      </Popover.Trigger>
      <EditorPopup>{editor(() => setOpen(false))}</EditorPopup>
    </Popover.Root>
  )
}

function EditorPopup({ children }: { children: React.ReactNode }) {
  return (
    <Popover.Portal>
      <Popover.Positioner className="isolate z-50" side="bottom" align="start" sideOffset={4} collisionPadding={8}>
        <Popover.Popup className="flex max-h-(--available-height) w-72 flex-col overflow-hidden bg-popover text-xs text-popover-foreground shadow-md ring-1 ring-foreground/10 outline-none">
          {children}
        </Popover.Popup>
      </Popover.Positioner>
    </Popover.Portal>
  )
}

/** A step's heading, with the way back to the step before when there is one. */
function StepHeader({ onBack, hint, children }: { onBack?: () => void; hint?: string; children: React.ReactNode }) {
  return (
    <div className="flex h-8 shrink-0 items-center gap-1 border-b pr-2 pl-1">
      {onBack ? (
        <button type="button" onClick={onBack} aria-label="Back" title="Back"
          className="flex h-6 w-6 shrink-0 items-center justify-center text-muted-foreground hover:bg-muted hover:text-foreground">
          <ChevronLeft className="h-3.5 w-3.5" />
        </button>
      ) : <span className="w-1" />}
      <span className="min-w-0 flex-1 truncate">{children}</span>
      {hint && <span className="shrink-0 text-[10px] text-muted-foreground">{hint}</span>}
    </div>
  )
}

/** A step's form: its controls, then a note on the left of Apply. Enter applies too. */
function StepForm({ onSubmit, disabled, note, children }: {
  onSubmit: () => void
  disabled: boolean
  note?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <form className="flex min-h-0 flex-col" onSubmit={e => { e.preventDefault(); if (!disabled) onSubmit() }}>
      <div className="flex min-h-0 flex-col gap-2 p-2">{children}</div>
      <div className="flex shrink-0 items-end gap-3 border-t px-2 py-1.5">
        <p className="min-w-0 flex-1 text-[11px] leading-snug text-muted-foreground">{note}</p>
        <Button type="submit" size="xs" disabled={disabled}>Apply</Button>
      </div>
    </form>
  )
}

/** One choice of a few, side by side. */
function Segmented<T extends string>({ label, options, value, onChange }: {
  label: string
  options: { value: T; label: string }[]
  value: T
  onChange: (value: T) => void
}) {
  return (
    <div role="radiogroup" aria-label={label} className="flex border">
      {options.map(o => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          onClick={() => onChange(o.value)}
          className={cn(
            'h-6 flex-1 border-l px-1.5 first:border-l-0',
            o.value === value ? 'bg-accent text-accent-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

const INPUT = 'h-7 w-full border bg-background px-2 text-xs outline-none placeholder:text-muted-foreground focus:border-ring'

/** A native select with our chevron, inset like the inputs' padding, in place of the browser's arrow. */
function NativeSelect(props: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <div className="relative">
      <select {...props} className={cn(INPUT, 'cursor-pointer appearance-none pr-7')} />
      <ChevronDown className="pointer-events-none absolute right-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
    </div>
  )
}

function ValueText({ value }: { value: ChipValue }) {
  return typeof value === 'string'
    ? <span className={STRING_TEXT}>"{value}"</span>
    : <span className={LITERAL_TEXT}>{String(value)}</span>
}

/** A filter as the Query reads it, its values coloured by type. */
function FilterText({ chip }: { chip: Chip }) {
  if (chip.op === 'exists' || chip.op === 'missing') {
    return <>{chip.field} <span className={MUTED_TEXT}>{chip.op === 'exists' ? 'exists' : 'is missing'}</span></>
  }
  if (Array.isArray(chip.value)) {
    return (
      <>
        {chip.field} <span className={MUTED_TEXT}>{chip.op} (</span>
        {chip.value.map((v, i) => <Fragment key={i}>{i > 0 && <span className={MUTED_TEXT}>, </span>}<ValueText value={v} /></Fragment>)}
        <span className={MUTED_TEXT}>)</span>
      </>
    )
  }
  return <>{chip.field} <span className={MUTED_TEXT}>{chip.op}</span> <ValueText value={chip.value as ChipValue} /></>
}

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

const OP_GROUPS: { label: string; ops: { op: FilterOp; label: string }[] }[] = [
  {
    label: 'Compare',
    ops: [
      { op: '==', label: '==  equals' },
      { op: '!=', label: '!=  doesn\'t equal' },
      { op: '>', label: '>  greater than' },
      { op: '>=', label: '>=  at least' },
      { op: '<', label: '<  less than' },
      { op: '<=', label: '<=  at most' },
    ],
  },
  {
    label: 'Text',
    ops: [
      { op: 'contains', label: 'contains' },
      { op: '!contains', label: 'doesn\'t contain' },
      { op: 'startswith', label: 'starts with' },
      { op: '!startswith', label: 'doesn\'t start with' },
    ],
  },
  {
    label: 'List',
    ops: [
      { op: 'in', label: 'in  (one of)' },
      { op: '!in', label: '!in  (none of)' },
    ],
  },
  {
    label: 'Presence',
    ops: [
      { op: 'exists', label: 'exists' },
      { op: 'missing', label: 'is missing' },
    ],
  },
]

const isTextOp = (op: FilterOp) => op.includes('contains') || op.includes('startswith')
const isListOp = (op: FilterOp) => op === 'in' || op === '!in'

/** The text a filter's value is edited as: a list comma-separated. */
function filterValueText(chip: Chip | undefined): string {
  if (!chip || chip.value === null) return ''
  if (Array.isArray(chip.value)) return chip.value.map(editableText).join(', ')
  return isTextOp(chip.op) ? String(chip.value) : editableText(chip.value)
}

/** The filter the editor holds, or null while it's incomplete. */
function filterOf(field: string, op: FilterOp, value: string): Chip | null {
  if (op === 'exists' || op === 'missing') return { field, op, value: null }
  if (isListOp(op)) {
    const values = listParts(value).map(parseValue)
    return values.length > 0 ? { field, op, value: values } : null
  }
  if (isTextOp(op)) return value === '' ? null : { field, op, value }
  return value.trim() === '' ? null : { field, op, value: parseValue(value) }
}

/** A word on how the filter matches, where it may not be what was meant. */
function filterNote(field: string, type: string | undefined, op: FilterOp, value: string): string | null {
  if (op === 'exists' || op === 'missing') return null
  if (op === 'contains' || op === '!contains') return 'Ignores case.'
  if (isTextOp(op)) return null
  const values = (isListOp(op) ? listParts(value) : [value]).filter(v => v.trim() !== '').map(parseValue)
  const text = values.find(v => typeof v === 'string')
  if (text !== undefined && (type === 'number' || type === 'bool')) {
    return `${field} holds ${type}s in recent Logs; "${text}" is matched as text.`
  }
  const typed = values.find(v => typeof v !== 'string')
  if (typed !== undefined && type === 'string') {
    return `${typed} is matched as a ${typeof typed === 'boolean' ? 'bool' : 'number'}. Quote it, as "${typed}", to match text.`
  }
  if (op === '!=' || op === '!in') return `Includes Logs without ${field}.`
  return null
}

/** Adds or edits a filter: pick the Field, then the operator and value. */
function FilterEditor({ chip, onApply }: { chip?: Chip; onApply: (chip: Chip) => void }) {
  const { fields, valuesOf } = useContext(SuggestionsContext)
  const [field, setField] = useState(chip?.field ?? '')
  const [picking, setPicking] = useState(!chip)
  const [op, setOp] = useState<FilterOp>(chip?.op ?? '==')
  const [value, setValue] = useState(filterValueText(chip))

  if (picking) {
    return (
      <PickList
        options={fieldOptions(fields)}
        chosen={field ? [field] : undefined}
        placeholder="Filter on a Field…"
        typed={TYPED_FIELD}
        empty="No Fields seen yet."
        onPick={name => {
          if (name !== field) setValue('')
          setField(name)
          setPicking(false)
        }}
      />
    )
  }
  return (
    <ConditionStep
      field={field}
      type={fields.find(f => f.name === field)?.type}
      values={valuesOf(field)}
      op={op}
      onOp={setOp}
      value={value}
      onValue={setValue}
      onBack={() => setPicking(true)}
      onApply={onApply}
    />
  )
}

function ConditionStep({ field, type, values, op, onOp, value, onValue, onBack, onApply }: {
  field: string
  type: string | undefined
  values: ChipValue[]
  op: FilterOp
  onOp: (op: FilterOp) => void
  value: string
  onValue: (value: string) => void
  onBack: () => void
  onApply: (chip: Chip) => void
}) {
  const needsValue = op !== 'exists' && op !== 'missing'
  const list = isListOp(op)
  const typed = valueOptions(values, isTextOp(op))
  // A type on every value says nothing when they all share it.
  const options = new Set(typed.map(o => o.hint)).size > 1 ? typed : typed.map(o => ({ ...o, hint: undefined }))
  const known = options.map(o => o.value)
  // A value picked shows the others still; a value being typed narrows them.
  const shown = list ? narrow(options, listFragment(value, known)) : narrow(options, known.includes(value.trim()) ? '' : value)
  const { active, setActive, onKeyDown } = useHighlight(shown, -1, o => pick(o))
  const next = filterOf(field, op, value)

  // A value applies the filter, as one click should; in a list it's added or taken out.
  function pick(o: PickOption) {
    if (!list) {
      const chip = filterOf(field, op, o.value)
      if (chip) onApply(chip)
      return
    }
    onValue(toggleListValue(value, o.value, known))
    setActive(-1)
  }

  return (
    <>
      <StepHeader onBack={onBack} hint={type}>{field}</StepHeader>
      <StepForm onSubmit={() => next && onApply(next)} disabled={!next} note={filterNote(field, type, op, value)}>
        <NativeSelect aria-label="Operator" value={op} onChange={e => onOp(e.target.value as FilterOp)} autoFocus={!needsValue}>
          {OP_GROUPS.map(g => (
            <optgroup key={g.label} label={g.label}>
              {g.ops.map(o => <option key={o.op} value={o.op}>{o.label}</option>)}
            </optgroup>
          ))}
        </NativeSelect>
        {needsValue && (
          <input
            autoFocus
            aria-label={list ? 'Values' : 'Value'}
            value={value}
            onChange={e => { onValue(e.target.value); setActive(-1) }}
            onKeyDown={onKeyDown}
            placeholder={list ? 'Values, comma-separated…' : 'Value…'}
            className={INPUT}
          />
        )}
        {needsValue && shown.length > 0 && (
          <div className="-mx-2 -mb-2 border-t">
            <p className="caps-label px-2 pt-2">Seen in recent Logs</p>
            <OptionList
              options={shown}
              active={active}
              onActive={setActive}
              onPick={pick}
              chosen={list ? listParts(value) : [value.trim()]}
              className="max-h-44"
            />
          </div>
        )}
      </StepForm>
    </>
  )
}

const FNS: PickOption[] = [
  { value: 'count', hint: 'Logs' },
  { value: 'rate', hint: 'Logs per second' },
  { value: 'peak_rate', hint: 'Logs in the busiest second' },
  { value: 'dcount', hint: 'distinct values' },
  { value: 'sum', hint: 'of a number' },
  { value: 'avg', hint: 'of a number' },
  { value: 'min', hint: 'of a number' },
  { value: 'max', hint: 'of a number' },
  { value: 'percentile', hint: 'of a number' },
]

const PERCENTS = ['50', '75', '90', '95', '99', '99.9']

/** Adds or edits a function: pick it, then its Field (and a percentile's percentage). */
function AggregateEditor({ aggregate, onApply }: { aggregate?: Aggregate; onApply: (a: Aggregate) => void }) {
  const { fields } = useContext(SuggestionsContext)
  const [fn, setFn] = useState<AggregateFn | null>(aggregate && !WITHOUT_FIELD.has(aggregate.fn) ? aggregate.fn : null)
  const [percent, setPercent] = useState(String(aggregate?.percent ?? 95))

  if (fn === null) {
    return (
      <MenuList
        label="Function"
        options={FNS}
        chosen={aggregate ? [aggregate.fn] : undefined}
        onPick={name => (WITHOUT_FIELD.has(name as AggregateFn) ? onApply({ fn: name as AggregateFn }) : setFn(name as AggregateFn))}
      />
    )
  }
  const field = aggregate?.fn === fn ? aggregate.field : undefined
  const apply = (name: string, p = percent) => onApply(fn === 'percentile' ? { fn, field: name, percent: Number(p) } : { fn, field: name })
  return (
    <>
      <StepHeader onBack={() => setFn(null)}>{fn}<span className={MUTED_TEXT}>(…)</span></StepHeader>
      {fn === 'percentile' && (
        <div className="border-b p-2">
          <Segmented
            label="Percentage"
            options={(PERCENTS.includes(percent) ? PERCENTS : [...PERCENTS, percent]).map(p => ({ value: p, label: `p${p}` }))}
            value={percent}
            onChange={p => {
              setPercent(p)
              // An existing percentile changes at once, as every change does.
              if (field) apply(field, p)
            }}
          />
        </div>
      )}
      <PickList
        options={fieldOptions(fields, fn === 'dcount' ? undefined : { types: ['number'], group: 'Numbers' })}
        chosen={field ? [field] : undefined}
        placeholder={`${fn} of a Field…`}
        typed={TYPED_FIELD}
        empty="No Fields seen yet."
        onPick={name => apply(name)}
      />
    </>
  )
}

const BINS = ['auto', '1m', '5m', '15m', '1h', '6h', '1d']
const BIN_OPTIONS: PickOption[] = BINS.map(bin => {
  const text = groupText({ field: 'time', bin })
  return { value: `bin:${bin}`, label: text, text, hint: bin === 'auto' ? 'follows the range' : undefined, group: 'Time' }
})

/** Adds or edits a by column: time in bins, or a Field. */
function GroupEditor({ group, onApply }: { group?: Group; onApply: (g: Group) => void }) {
  const { fields } = useContext(SuggestionsContext)
  const chosen = group ? (group.bin ? `bin:${group.bin}` : group.field) : undefined
  return (
    <PickList
      options={[...BIN_OPTIONS, ...fieldOptions(fields, { types: ['string', 'bool'], group: 'Fields' })]}
      chosen={chosen ? [chosen] : undefined}
      placeholder="Group by time or a Field…"
      typed={TYPED_FIELD}
      empty="Nothing to group by."
      onPick={value => onApply(value.startsWith('bin:') ? { field: 'time', bin: value.slice(4) } : { field: value })}
    />
  )
}

const MORE = ['search', 'sort', 'limit'] as const
type More = (typeof MORE)[number]

/** "a", "a or b", "a, b or c". */
function listText(words: string[]): string {
  return words.length < 2 ? words.join('') : `${words.slice(0, -1).join(', ')} or ${words.at(-1)}`
}

/** Adds a search, sort or limit, those of kinds; straight into its step when there's one. */
function MoreEditor({ kinds, columns, onSearch, onSort, onLimit }: {
  kinds: readonly More[]
  columns: string[] | null
  onSearch: (text: string) => void
  onSort: (key: SortKey) => void
  onLimit: (n: number) => void
}) {
  const [kind, setKind] = useState<More | null>(kinds.length === 1 ? kinds[0] : null)
  const back = kinds.length > 1 ? () => setKind(null) : undefined
  if (kind === 'search') return <SearchStep onBack={back} onApply={onSearch} />
  if (kind === 'sort') return <SortStep columns={columns} onBack={back} onApply={onSort} />
  if (kind === 'limit') return <LimitStep onBack={back} onApply={onLimit} />
  const options: Record<More, PickOption> = {
    search: { value: 'search', hint: 'text anywhere' },
    sort: { value: 'sort', label: 'sort by', hint: 'order the rows' },
    limit: { value: 'limit', hint: 'keep the first rows' },
  }
  return <MenuList label="Add" options={kinds.map(k => options[k])} onPick={value => setKind(value as More)} />
}

/** Searches the message and every string Attribute, ignoring case: the search stage. */
function SearchStep({ text: initial, onBack, onApply }: { text?: string; onBack?: () => void; onApply: (text: string) => void }) {
  const [text, setText] = useState(initial ?? '')
  return (
    <>
      <StepHeader onBack={onBack}>search</StepHeader>
      <StepForm onSubmit={() => onApply(text)} disabled={text.trim() === ''} note="Finds the text in the message or any text Attribute, ignoring case.">
        <input autoFocus aria-label="Text" value={text} onChange={e => setText(e.target.value)} placeholder="Text to find…" className={INPUT} />
      </StepForm>
    </>
  )
}

/** columns: what a Summarize makes, or null to sort Logs by any Field. */
function SortStep({ columns, sortKey, onBack, onApply }: {
  columns: string[] | null
  sortKey?: SortKey
  onBack?: () => void
  onApply: (key: SortKey) => void
}) {
  const { fields } = useContext(SuggestionsContext)
  const [asc, setAsc] = useState(sortKey?.asc ?? false)
  return (
    <>
      <StepHeader onBack={onBack}>sort by</StepHeader>
      <div className="border-b p-2">
        <Segmented
          label="Order"
          options={[{ value: 'desc', label: 'descending' }, { value: 'asc', label: 'ascending' }]}
          value={asc ? 'asc' : 'desc'}
          onChange={order => {
            setAsc(order === 'asc')
            // An existing sort changes at once, as every change does.
            if (sortKey) onApply({ column: sortKey.column, asc: order === 'asc' })
          }}
        />
      </div>
      <PickList
        options={columns ? columns.map(c => ({ value: c, group: 'Summarize columns' })) : fieldOptions(fields)}
        chosen={sortKey ? [sortKey.column] : undefined}
        placeholder={columns ? 'Sort by a column…' : 'Sort by a Field…'}
        typed={columns ? undefined : TYPED_FIELD}
        empty={columns ? 'No columns match.' : 'No Fields seen yet.'}
        onPick={column => onApply({ column, asc })}
      />
      {!columns && <p className="border-t px-2 py-1.5 text-[11px] text-muted-foreground">Logs sorted by a Field show as a table.</p>}
    </>
  )
}

const LIMITS = [10, 100, 1000, 10000]

function LimitStep({ take, onBack, onApply }: { take?: number; onBack?: () => void; onApply: (n: number) => void }) {
  const [text, setText] = useState(String(take ?? 100))
  const n = Number(text)
  const ok = text.trim() !== '' && Number.isInteger(n) && n >= 0
  return (
    <>
      <StepHeader onBack={onBack}>limit</StepHeader>
      <StepForm onSubmit={() => onApply(n)} disabled={!ok} note="Keeps the first rows, in the order shown.">
        <input autoFocus aria-label="Rows" inputMode="numeric" value={text} onChange={e => setText(e.target.value)} className={INPUT} />
        <div className="flex gap-1">
          {LIMITS.map(l => (
            <button key={l} type="button" onClick={() => onApply(l)}
              className="h-6 flex-1 border text-muted-foreground hover:bg-muted hover:text-foreground">
              {l.toLocaleString()}
            </button>
          ))}
        </div>
      </StepForm>
    </>
  )
}
