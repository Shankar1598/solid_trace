// The Builder: Where filters, a search, then Summarize, sort and limit.
// Its state is a Query: it is written here as Query text, and read back only
// from validate's chips, so the browser never parses Queries.

/** A chip's value keeps its type: `status == 500` and `status == "500"` differ. */
export type ChipValue = string | number | boolean

export type FilterOp =
  | '==' | '!=' | '>' | '>=' | '<' | '<='
  | 'contains' | '!contains' | 'startswith' | '!startswith'
  | 'in' | '!in' | 'exists' | 'missing'

/** A chip's value that is one of a Dashboard's filters, written `$name`. */
export interface FilterValue {
  filter: string
}

/**
 * One Where filter: a Field, an operator and a value. in and !in take a
 * list; exists and missing (isnotnull and isnull) take none. ==, != and the
 * text operators can take a Dashboard filter instead of a value.
 */
export interface Chip {
  field: string
  op: FilterOp
  value: ChipValue | FilterValue | ChipValue[] | null
}

export function isFilterValue(value: Chip['value']): value is FilterValue {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export type AggregateFn = 'count' | 'rate' | 'peak_rate' | 'dcount' | 'sum' | 'avg' | 'min' | 'max' | 'percentile'

/** The functions that take no Field: count(), rate() and peak_rate(). */
export const WITHOUT_FIELD: ReadonlySet<AggregateFn> = new Set(['count', 'rate', 'peak_rate'])

/** One of Summarize's functions: count(), rate() and peak_rate() have no Field; percentile has its percentage. */
export interface Aggregate {
  fn: AggregateFn
  field?: string
  percent?: number
}

/** One of Summarize's by columns: a Field, or time in bins of `bin` (such as 5m, or auto). */
export interface Group {
  field: string
  bin?: string
}

export interface Summarize {
  aggregates: Aggregate[]
  by: Group[]
}

/** One sort key: a column, descending unless asc. */
export interface SortKey {
  column: string
  asc: boolean
}

/** A Query as validate reads it for the Builder. */
export interface Chips {
  filters: Chip[]
  search: string | null
  /** The Dashboard filter the search is, as in `search $q`; search is null then. */
  search_filter?: string | null
  summarize?: Summarize | null
  sort?: SortKey[]
  take?: number | null
}

export interface Builder {
  filters: Chip[]
  search: string
  /** A Dashboard filter the search is, `search $q`, in place of the search text. */
  searchFilter?: string
  /** Absent for no Summarize, and likewise sort and take. */
  summarize?: Summarize
  sort?: SortKey[]
  take?: number
}

export const EMPTY_BUILDER: Builder = { filters: [], search: '' }

/**
 * The Query text: the Project's slug as its source, then one where stage per
 * chip, in order, then the search, then any summarize, sort and take.
 */
export function queryOf(builder: Builder, projectSlug: string): string {
  const stages = [sourceText(projectSlug), ...builder.filters.map(filterText)]
  if (builder.searchFilter) stages.push(`search $${builder.searchFilter}`)
  else if (builder.search.trim() !== '') stages.push(`search ${stringText(builder.search)}`)
  const summarize = builder.summarize && summarizeText(builder.summarize)
  if (summarize) stages.push(summarize)
  if (builder.sort && builder.sort.length > 0) {
    stages.push(`sort by ${builder.sort.map(k => `${nameText(k.column)} ${k.asc ? 'asc' : 'desc'}`).join(', ')}`)
  }
  if (builder.take !== undefined) stages.push(`take ${builder.take}`)
  return stages.join('\n| ')
}

/** A Project's slug as a Query's source, in brackets: `['my-api']`. */
function sourceText(slug: string): string {
  return `['${escape(slug).replaceAll("'", "\\'")}']`
}

/** One chip as its where stage, such as `where service == "api"`, or `where service == $service`. */
export function filterText(chip: Chip): string {
  const field = nameText(chip.field)
  if (isFilterValue(chip.value)) return `where ${field} ${chip.op} $${chip.value.filter}`
  switch (chip.op) {
    case 'exists':
      return `where isnotnull(${field})`
    case 'missing':
      return `where isnull(${field})`
    case 'in':
    case '!in':
      return `where ${field} ${chip.op} (${listOf(chip.value).map(valueText).join(', ')})`
    case 'contains':
    case '!contains':
    case 'startswith':
    case '!startswith':
      return `where ${field} ${chip.op} ${stringText(String(listOf(chip.value)[0] ?? ''))}`
    default:
      return `where ${field} ${chip.op} ${valueText(listOf(chip.value)[0] ?? '')}`
  }
}

function summarizeText(summarize: Summarize): string | null {
  if (summarize.aggregates.length === 0 && summarize.by.length === 0) return null
  const aggregates = summarize.aggregates.map(aggregateText).join(', ')
  const by = summarize.by.map(groupText).join(', ')
  if (by === '') return `summarize ${aggregates}`
  return aggregates === '' ? `summarize by ${by}` : `summarize ${aggregates} by ${by}`
}

export function aggregateText(a: Aggregate): string {
  if (WITHOUT_FIELD.has(a.fn)) return `${a.fn}()`
  const field = nameText(a.field ?? '')
  return a.fn === 'percentile' ? `percentile(${field}, ${a.percent ?? 95})` : `${a.fn}(${field})`
}

export function groupText(g: Group): string {
  if (g.bin === 'auto') return 'bin_auto(time)'
  if (g.bin) return `bin(time, ${g.bin})`
  return nameText(g.field)
}

/** The column an aggregate makes, as the language names it: count_, avg_duration, percentile_duration_95. */
export function aggregateColumn(a: Aggregate): string {
  const name = `${a.fn}_${a.field ?? ''}`
  return a.fn === 'percentile' ? `${name}_${a.percent ?? 95}` : name
}

/** The columns a Summarize makes, by columns first, which sort can order by. */
export function summarizeColumns(summarize: Summarize): string[] {
  return [...summarize.by.map(g => g.field), ...summarize.aggregates.map(aggregateColumn)]
}

export function builderOf(chips: Chips): Builder {
  const builder: Builder = { filters: chips.filters, search: chips.search ?? '' }
  if (chips.search_filter) builder.searchFilter = chips.search_filter
  if (chips.summarize) builder.summarize = chips.summarize
  if (chips.sort && chips.sort.length > 0) builder.sort = chips.sort
  if (chips.take !== undefined && chips.take !== null) builder.take = chips.take
  return builder
}

/** Each operator's opposite, which the chip's operator flips to. */
const OPPOSITE: Record<FilterOp, FilterOp> = {
  '==': '!=', '!=': '==', '>': '<=', '<=': '>', '<': '>=', '>=': '<',
  contains: '!contains', '!contains': 'contains', startswith: '!startswith', '!startswith': 'startswith',
  in: '!in', '!in': 'in', exists: 'missing', missing: 'exists',
}

export function flipFilter(builder: Builder, index: number): Builder {
  return {
    ...builder,
    filters: builder.filters.map((f, i) => (i === index ? { ...f, op: OPPOSITE[f.op] } : f)),
  }
}

/** Puts chip in place of the filter at index, or adds it at the end when index is the length. */
export function setFilter(builder: Builder, index: number, chip: Chip): Builder {
  const filters = [...builder.filters]
  filters[index] = chip
  return { ...builder, filters }
}

/**
 * Sets the Summarize, or removes it (and a sort on its columns) when it is
 * empty. A limit goes when a Summarize starts or ends: it counted Logs, or
 * the old table's rows, and would cut the new answer short.
 */
export function setSummarize(builder: Builder, summarize: Summarize): Builder {
  const empty = summarize.aggregates.length === 0 && summarize.by.length === 0
  const { summarize: _old, sort, take, ...rest } = builder
  const next: Builder = empty ? rest : { ...rest, summarize }
  if (take !== undefined && empty === !builder.summarize) next.take = take
  // A sort on a column the Summarize no longer makes would fail.
  const columns = empty ? null : summarizeColumns(summarize)
  const kept = (sort ?? []).filter(k => (columns === null ? !builder.summarize : columns.includes(k.column)))
  if (kept.length > 0) next.sort = kept
  return next
}

export function setSort(builder: Builder, sort: SortKey[]): Builder {
  const { sort: _old, ...rest } = builder
  return sort.length > 0 ? { ...rest, sort } : rest
}

export function setTake(builder: Builder, take: number | null): Builder {
  const { take: _old, ...rest } = builder
  return take === null ? rest : { ...rest, take }
}

export function removeFilter(builder: Builder, index: number): Builder {
  return { ...builder, filters: builder.filters.filter((_, i) => i !== index) }
}

/** Sets the search text, in place of any search filter. */
export function search(builder: Builder, text: string): Builder {
  const { searchFilter: _old, ...rest } = builder
  return { ...rest, search: text }
}

/** Makes the search a Dashboard's search filter, `search $name`, in place of any text. */
export function searchByFilter(builder: Builder, name: string): Builder {
  return { ...builder, search: '', searchFilter: name }
}

/**
 * Show only this: field == value. It replaces any chip on the same value and
 * the Field's other == chips, which together would match nothing.
 */
export function showOnly(builder: Builder, field: string, value: ChipValue): Builder {
  const kept = builder.filters.filter(f => f.field !== field || (f.op === '!=' && f.value !== value))
  return { ...builder, filters: [...kept, { field, op: '==', value }] }
}

/** Hide this: field != value, which keeps Logs that don't have the Field. */
export function hide(builder: Builder, field: string, value: ChipValue): Builder {
  const kept = builder.filters.filter(f => f.field !== field || f.value !== value)
  return { ...builder, filters: [...kept, { field, op: '!=', value }] }
}

/**
 * The Builder with a filter added from a value: == and != as Show only this and
 * Hide this add them, any other unless the Builder has it already.
 */
export function addFilter(builder: Builder, chip: Chip): Builder {
  if (chip.op === '==' && canFilter(chip.value)) return showOnly(builder, chip.field, chip.value)
  if (chip.op === '!=' && canFilter(chip.value)) return hide(builder, chip.field, chip.value)
  const same = (f: Chip) => f.field === chip.field && f.op === chip.op && JSON.stringify(f.value) === JSON.stringify(chip.value)
  return builder.filters.some(same) ? builder : { ...builder, filters: [...builder.filters, chip] }
}

/**
 * What a new Summarize groups by before anything else: time in automatic
 * bins, so its first function draws a chart over time.
 */
const NEW_SUMMARIZE: Summarize = { aggregates: [], by: [{ field: 'time', bin: 'auto' }] }

/** Adds a function to the Summarize, starting one by bin_auto(time) when there is none. */
export function addAggregate(builder: Builder, aggregate: Aggregate): Builder {
  const summarize = builder.summarize ?? NEW_SUMMARIZE
  return setSummarize(builder, { ...summarize, aggregates: [...summarize.aggregates, aggregate] })
}

/**
 * Removes a function from the Summarize. Removing the last one drops the
 * automatic time bins a new Summarize starts with, which without a function
 * would only list distinct bins; by columns of the user's own stay. So Add a
 * function and its x leave the Builder as it was.
 */
export function removeAggregate(builder: Builder, index: number): Builder {
  const summarize = builder.summarize
  if (!summarize) return builder
  const aggregates = summarize.aggregates.filter((_, i) => i !== index)
  const by = aggregates.length === 0 ? summarize.by.filter(g => g.bin !== 'auto') : summarize.by
  return setSummarize(builder, { aggregates, by })
}

/** Counts Logs by a Field: one more by column of the Summarize, or a new count() by time and it. */
export function groupBy(builder: Builder, field: string): Builder {
  const summarize = builder.summarize ?? NEW_SUMMARIZE
  const by = summarize.by.some(g => g.field === field && !g.bin) ? summarize.by : [...summarize.by, { field }]
  const aggregates: Aggregate[] = summarize.aggregates.length > 0 ? summarize.aggregates : [{ fn: 'count' }]
  return setSummarize(builder, { aggregates, by })
}

/**
 * Chart this: a Summarize that charts a Field over time, in place of any
 * other. A number's Field charts its average; any other counts Logs by the
 * Field's values.
 */
export function chartField(builder: Builder, field: string, numeric: boolean): Builder {
  const time: Group = { field: 'time', bin: 'auto' }
  // A fresh chart: a limit on the old Summarize's rows would cut its bins.
  return setSummarize(setTake(builder, null), numeric
    ? { aggregates: [{ fn: 'avg', field }], by: [time] }
    : { aggregates: [{ fn: 'count' }], by: [{ field }, time] })
}

/** Whether a value can be a chip: Show only this and Hide this need one. */
export function canFilter(value: unknown): value is ChipValue {
  return typeof value === 'string' || typeof value === 'boolean' || (typeof value === 'number' && Number.isFinite(value))
}

// Names the language reads as words, not Fields.
const RESERVED = new Set(['and', 'or', 'not', 'true', 'false'])
// $ goes in a name but starts a Dashboard filter, so a name starting with $ is quoted.
const PLAIN_PART = /^[A-Za-z_][A-Za-z0-9_$]*$/

/** A Field's name: bare when it's plain (dotted parts and all), else ['quoted']. */
export function nameText(name: string): string {
  const plain = name.split('.').every(part => PLAIN_PART.test(part) && !RESERVED.has(part))
  return plain ? name : `['${escape(name).replaceAll("'", "\\'")}']`
}

/** A value as a Query writes it: strings quoted, numbers and bools bare. */
export function valueText(value: ChipValue): string {
  return typeof value === 'string' ? stringText(value) : String(value)
}

function listOf(value: ChipValue | ChipValue[] | null): ChipValue[] {
  if (value === null) return []
  return Array.isArray(value) ? value : [value]
}

/**
 * A value typed in an editor: true and false are bools, numbers are numbers,
 * and anything else a string. Quotes make a string of anything, as "500".
 */
export function parseValue(text: string): ChipValue {
  const trimmed = text.trim()
  const quoted = /^"(.*)"$/s.exec(trimmed) ?? /^'(.*)'$/s.exec(trimmed)
  if (quoted) return quoted[1]
  if (trimmed === 'true' || trimmed === 'false') return trimmed === 'true'
  if (trimmed !== '' && Number.isFinite(Number(trimmed))) return Number(trimmed)
  return text
}

/** A value as an editor shows it: parseValue reads it back the same. */
export function editableText(value: ChipValue): string {
  if (typeof value !== 'string') return String(value)
  return parseValue(value) === value ? value : `"${value}"`
}

function stringText(text: string): string {
  return `"${escape(text).replaceAll('"', '\\"')}"`
}

function escape(text: string): string {
  return text.replaceAll('\\', '\\\\').replaceAll('\n', '\\n').replaceAll('\t', '\\t').replaceAll('\r', '\\r')
}
