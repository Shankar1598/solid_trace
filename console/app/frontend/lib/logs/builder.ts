// The Builder: filter chips and a search box. Its state is a Query: chips and
// search are written here as Query text, and read back only from validate's
// chips, so the browser never parses Queries.

/** A chip's value keeps its type: `status == 500` and `status == "500"` differ. */
export type ChipValue = string | number | boolean

/** One filter chip: `field = value` (==) or `field ≠ value` (!=). */
export interface Chip {
  field: string
  op: '==' | '!='
  value: ChipValue
}

/** A Query as validate reads it for the Builder. */
export interface Chips {
  filters: Chip[]
  search: string | null
}

export interface Builder {
  filters: Chip[]
  search: string
}

export const EMPTY_BUILDER: Builder = { filters: [], search: '' }

/**
 * The Query text: the Project's slug as its source, then one where stage per
 * chip, in order, then the search.
 */
export function queryOf(builder: Builder, projectSlug: string): string {
  const stages = [sourceText(projectSlug), ...builder.filters.map(filterText)]
  if (builder.search.trim() !== '') stages.push(`search ${stringText(builder.search)}`)
  return stages.join('\n| ')
}

// The words that start a stage: a slug spelled as one is written ['quoted'],
// so `count` stays a source and not the count stage.
const STAGES = new Set(['where', 'search', 'project', 'extend', 'summarize', 'sort', 'order', 'take', 'limit', 'top', 'count', 'distinct'])
const PLAIN_SLUG = /^[A-Za-z0-9_][A-Za-z0-9_-]*$/

/** A Project's slug as a Query's source: bare, such as `my-api`, unless it can't be. */
function sourceText(slug: string): string {
  return PLAIN_SLUG.test(slug) && !STAGES.has(slug) ? slug : `['${escape(slug).replaceAll("'", "\\'")}']`
}

/** One chip as its where stage, such as `where service == "api"`. */
export function filterText(chip: Chip): string {
  return `where ${nameText(chip.field)} ${chip.op} ${valueText(chip.value)}`
}

export function builderOf(chips: Chips): Builder {
  return { filters: chips.filters, search: chips.search ?? '' }
}

export function flipFilter(builder: Builder, index: number): Builder {
  return {
    ...builder,
    filters: builder.filters.map((f, i) => (i === index ? { ...f, op: f.op === '==' ? '!=' : '==' } : f)),
  }
}

export function removeFilter(builder: Builder, index: number): Builder {
  return { ...builder, filters: builder.filters.filter((_, i) => i !== index) }
}

export function search(builder: Builder, text: string): Builder {
  return { ...builder, search: text }
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

/** Whether a value can be a chip: Show only this and Hide this need one. */
export function canFilter(value: unknown): value is ChipValue {
  return typeof value === 'string' || typeof value === 'boolean' || (typeof value === 'number' && Number.isFinite(value))
}

// Names the language reads as words, not Fields.
const RESERVED = new Set(['and', 'or', 'not', 'true', 'false'])
const PLAIN_PART = /^[A-Za-z_$][A-Za-z0-9_$]*$/

/** A Field's name: bare when it's plain (dotted parts and all), else ['quoted']. */
function nameText(name: string): string {
  const plain = name.split('.').every(part => PLAIN_PART.test(part) && !RESERVED.has(part))
  return plain ? name : `['${escape(name).replaceAll("'", "\\'")}']`
}

function valueText(value: ChipValue): string {
  return typeof value === 'string' ? stringText(value) : String(value)
}

function stringText(text: string): string {
  return `"${escape(text).replaceAll('"', '\\"')}"`
}

function escape(text: string): string {
  return text.replaceAll('\\', '\\\\').replaceAll('\n', '\\n').replaceAll('\t', '\\t').replaceAll('\r', '\\r')
}
