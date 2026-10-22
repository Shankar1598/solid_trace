import { useEffect, useState } from 'react'
import { SlidersHorizontal, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput, InputGroupText } from '@/components/ui/input-group'
import { Select, SelectContent, SelectItem, SelectSeparator, SelectTrigger } from '@/components/ui/select'
import { chosenText, type DashboardFilter, type FilterValues } from '@/lib/dashboards/dashboard'

type SelectFilter = Extract<DashboardFilter, { kind: 'select' }>
type SearchFilter = Extract<DashboardFilter, { kind: 'search' }>

/** All, in a select: options are never empty, so '' stands for it. */
const ALL = ''

/**
 * The Dashboard's filters under its header, each a select or a search box
 * whose values the charts that name it run with, and the Filters button that
 * edits them. Every filter is All until a value is picked.
 */
export function FilterBar({ filters, values, onChange, onEdit }: {
  filters: DashboardFilter[]
  values: FilterValues
  /** A filter's new values; none is All. */
  onChange: (name: string, values: string[]) => void
  onEdit: () => void
}) {
  return (
    <div className="mb-3 flex flex-wrap items-center gap-2" data-filter-bar>
      {filters.map(filter => filter.kind === 'select' ? (
        <SelectControl key={filter.name} filter={filter} values={values[filter.name] ?? []} onChange={next => onChange(filter.name, next)} />
      ) : (
        <SearchControl key={filter.name} filter={filter} value={values[filter.name]?.[0] ?? ''} onChange={next => onChange(filter.name, next ? [next] : [])} />
      ))}
      <Button variant="outline" size="sm" className="h-8" onClick={onEdit}>
        <SlidersHorizontal className="h-3.5 w-3.5" /> Filters
      </Button>
    </div>
  )
}

/** A select: All at the top, then its options, one value or several. */
function SelectControl({ filter, values, onChange }: { filter: SelectFilter; values: string[]; onChange: (values: string[]) => void }) {
  const trigger = (
    <SelectTrigger size="sm" className="h-8 max-w-72" aria-label={filter.label}>
      <span className="truncate"><span className="text-muted-foreground">{filter.label}:</span> {chosenText(filter, values)}</span>
    </SelectTrigger>
  )
  const items = (
    <SelectContent>
      <SelectItem value={ALL}>All</SelectItem>
      <SelectSeparator />
      {filter.options.items.map(option => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
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
