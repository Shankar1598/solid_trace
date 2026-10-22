import { useEffect, useRef, useState } from 'react'
import { SlidersHorizontal, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Combobox, ComboboxContent, ComboboxEmpty, ComboboxInput, ComboboxItem, ComboboxList, ComboboxTrigger } from '@/components/ui/combobox'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput, InputGroupText } from '@/components/ui/input-group'
import { Select, SelectContent, SelectItem, SelectSeparator, SelectTrigger } from '@/components/ui/select'
import { fetchFilterValues } from '@/lib/dashboards/api'
import {
  chosenText, fieldOptions, type DashboardFilter, type FieldValue, type FilterOption, type FilterValues,
} from '@/lib/dashboards/dashboard'

type SelectFilter = Extract<DashboardFilter, { kind: 'select' }>
type SearchFilter = Extract<DashboardFilter, { kind: 'search' }>

/** All, in a select: options are never empty, so '' stands for it. */
const ALL = ''

/** The times the Dashboard last ran over, which a field select's values are read from. */
interface Shown {
  from: Date
  to: Date
}

/**
 * The Dashboard's filters under its header, each a select or a search box
 * whose values the charts that name it run with, and the Filters button that
 * edits them. Every filter is All until a value is picked.
 */
export function FilterBar({ filters, values, orgSlug, dashboardId, shown, onChange, onEdit }: {
  filters: DashboardFilter[]
  values: FilterValues
  orgSlug: string
  dashboardId: number
  shown: Shown
  /** A filter's new values; none is All. */
  onChange: (name: string, values: string[]) => void
  onEdit: () => void
}) {
  return (
    <div className="mb-3 flex flex-wrap items-center gap-2" data-filter-bar>
      {filters.map(filter => {
        const own = values[filter.name] ?? []
        const change = (next: string[]) => onChange(filter.name, next)
        if (filter.kind === 'search') {
          return <SearchControl key={filter.name} filter={filter} value={own[0] ?? ''} onChange={next => change(next ? [next] : [])} />
        }
        if (filter.options.from === 'field') {
          const load = (signal: AbortSignal) => fetchFilterValues({ orgSlug, dashboardId, name: filter.name, from: shown.from, to: shown.to, signal })
          return <FieldSelectControl key={filter.name} filter={filter} values={own} shown={shown} load={load} onChange={change} />
        }
        return <SelectControl key={filter.name} filter={filter} items={filter.options.items} values={own} onChange={change} />
      })}
      <Button variant="outline" size="sm" className="h-8" onClick={onEdit}>
        <SlidersHorizontal className="h-3.5 w-3.5" /> Filters
      </Button>
    </div>
  )
}

/** A select of a fixed list: All at the top, then its options, one value or several. */
function SelectControl({ filter, items: options, values, onChange }: {
  filter: SelectFilter; items: FilterOption[]; values: string[]; onChange: (values: string[]) => void
}) {
  const trigger = (
    <SelectTrigger size="sm" className="h-8 max-w-72" aria-label={filter.label}>
      <span className="truncate"><span className="text-muted-foreground">{filter.label}:</span> {chosenText(filter, values)}</span>
    </SelectTrigger>
  )
  const items = (
    <SelectContent>
      <SelectItem value={ALL}>All</SelectItem>
      <SelectSeparator />
      {options.map(option => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
    </SelectContent>
  )

  if (!filter.multiple) {
    return (
      <Select value={values[0] ?? ALL} onValueChange={next => onChange(next ? [next] : [])}>
        {trigger}
        {items}
      </Select>
    )
  }
  // All is ticked while nothing else is; ticking it clears the others.
  return (
    <Select
      multiple
      value={values.length > 0 ? values : [ALL]}
      onValueChange={next => onChange(next.includes(ALL) && values.length > 0 ? [] : next.filter(value => value !== ALL))}
    >
      {trigger}
      {items}
    </Select>
  )
}

/** A field select's values as its dropdown last loaded them, and whether a load is running or failed. */
type Loaded = { status: 'loading' | 'loaded'; values: FieldValue[] } | { status: 'failed'; error: string; values: FieldValue[] }

const labelOf = (value: string) => (value === ALL ? 'All' : value)

/**
 * A select of a field's values: All, then the field's most common values over
 * the times the Dashboard shows, loaded when the dropdown opens and again when
 * it opens after those times changed, so a Dashboard opens without loading
 * them. Typing narrows the list.
 */
function FieldSelectControl({ filter, values, shown, load, onChange }: {
  filter: SelectFilter
  values: string[]
  shown: Shown
  load: (signal: AbortSignal) => Promise<FieldValue[]>
  onChange: (values: string[]) => void
}) {
  const [loaded, setLoaded] = useState<Loaded | null>(null)
  const loadedOver = useRef<string | null>(null)
  const abort = useRef<AbortController | null>(null)
  useEffect(() => () => abort.current?.abort(), [])

  const opened = () => {
    const over = `${shown.from.toISOString()}/${shown.to.toISOString()}`
    if (loadedOver.current === over) return
    loadedOver.current = over
    abort.current?.abort()
    const controller = new AbortController()
    abort.current = controller
    setLoaded(prev => ({ status: 'loading', values: prev?.values ?? [] }))
    load(controller.signal).then(
      next => setLoaded({ status: 'loaded', values: next }),
      error => {
        if (controller.signal.aborted) return
        loadedOver.current = null // Opening it again tries again.
        setLoaded({ status: 'failed', error: error instanceof Error ? error.message : String(error), values: [] })
      },
    )
  }

  const options = fieldOptions(loaded?.values ?? [], values)
  const counts = new Map(options.map(o => [o.value, o.count]))
  const shared = {
    items: [ALL, ...options.map(o => o.value)],
    itemToStringLabel: labelOf,
    autoHighlight: true,
    onOpenChange: (open: boolean) => { if (open) opened() },
  }
  const parts = (
    <>
      <ComboboxTrigger
        aria-label={filter.label}
        className="border-input dark:bg-input/30 dark:hover:bg-input/50 focus-visible:border-ring focus-visible:ring-ring/50 flex h-8 w-fit max-w-72 items-center gap-1.5 border bg-transparent py-2 pr-2 pl-2.5 text-xs whitespace-nowrap outline-none focus-visible:ring-1"
      >
        <span className="truncate"><span className="text-muted-foreground">{filter.label}:</span> {chosenText(filter, values)}</span>
      </ComboboxTrigger>
      <ComboboxContent className="min-w-64">
        <ComboboxInput showTrigger={false} placeholder="Narrow the values" aria-label={`Narrow ${filter.label}'s values`} />
        {loaded?.status === 'loading' && <p role="status" className="px-2 py-2 text-muted-foreground">Loading values…</p>}
        {loaded?.status === 'failed' && <p role="alert" className="px-2 py-2 text-destructive">{loaded.error}</p>}
        {loaded?.status === 'loaded' && loaded.values.length === 0 && (
          <p className="px-2 py-2 text-muted-foreground">No Logs in this range hold {filter.label}.</p>
        )}
        <ComboboxEmpty>No values match.</ComboboxEmpty>
        <ComboboxList>
          {(value: string) => (
            <ComboboxItem key={value} value={value}>
              <span className="truncate">{labelOf(value)}</span>
              {counts.get(value) != null && <span className="ml-auto pl-4 text-muted-foreground tabular-nums">{counts.get(value)!.toLocaleString()}</span>}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </>
  )

  if (!filter.multiple) {
    return (
      <Combobox {...shared} value={values[0] ?? ALL} onValueChange={next => onChange(next ? [next] : [])}>
        {parts}
      </Combobox>
    )
  }
  // All is ticked while nothing else is; ticking it clears the others.
  return (
    <Combobox
      {...shared}
      multiple
      value={values.length > 0 ? values : [ALL]}
      onValueChange={next => onChange(next.includes(ALL) && values.length > 0 ? [] : next.filter(value => value !== ALL))}
    >
      {parts}
    </Combobox>
  )
}

/** A search box: its text runs on Enter, and clearing it is All. */
function SearchControl({ filter, value, onChange }: { filter: SearchFilter; value: string; onChange: (value: string) => void }) {
  const [draft, setDraft] = useState(value)
  useEffect(() => setDraft(value), [value])

  return (
    <InputGroup className="h-8 w-60">
      <InputGroupAddon><InputGroupText className="font-normal text-muted-foreground">{filter.label}:</InputGroupText></InputGroupAddon>
      <InputGroupInput
        aria-label={filter.label}
        placeholder="All"
        value={draft}
        onChange={e => setDraft(e.target.value)}
        onKeyDown={e => { if (e.key === 'Enter') onChange(draft.trim()) }}
      />
      {value && (
        <InputGroupAddon align="inline-end">
          <InputGroupButton aria-label={`Clear ${filter.label}`} onClick={() => onChange('')}><X /></InputGroupButton>
        </InputGroupAddon>
      )}
    </InputGroup>
  )
}
