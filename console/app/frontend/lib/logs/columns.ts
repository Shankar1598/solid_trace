// The columns the stream shows its Logs in: built-in fields and Attributes,
// in the reader's order. They live in the URL (one `col` per column, left to
// right) beside the explorer's state, so a shared link shows the same columns;
// changing them replaces the history entry rather than adding one.
import type { AttributeValue, Log } from './stream'

export const DEFAULT_COLUMNS = ['time', 'service', 'level', 'message']

/** The built-in fields, in the order the column picker lists them. */
export const BUILT_IN_FIELDS = ['time', 'level', 'service', 'environment', 'message', 'trace_id', 'span_id'] as const
type BuiltInField = (typeof BUILT_IN_FIELDS)[number]

/** Logs read for the picker's Attributes: enough to find the usual ones. */
const PICKER_LOGS = 500

/** Logs kept across runs for the Builder to suggest Fields and values from. */
const SUGGEST_LOGS = 2000

export function columnsFromSearch(search: string): string[] {
  const columns = unique(new URLSearchParams(search).getAll('col').map(c => c.trim()).filter(c => c !== ''))
  return columns.length > 0 ? columns : DEFAULT_COLUMNS
}

/** search with its columns set to columns; the default columns leave no trace. */
export function withColumns(search: string, columns: string[]): string {
  const params = new URLSearchParams(search)
  params.delete('col')
  if (!sameColumns(columns, DEFAULT_COLUMNS)) columns.forEach(c => params.append('col', c))
  const text = params.toString()
  return text === '' ? '' : `?${text}`
}

/** A Log's value for a column: a built-in field, else the Attribute of that name. */
export function columnValue(log: Log, column: string): AttributeValue | undefined {
  if (isBuiltIn(column)) return log[column]
  return log.attributes?.[column]
}

/** Adds column at the right end, or removes it; the last column stays. */
export function toggleColumn(columns: string[], column: string): string[] {
  if (!columns.includes(column)) return [...columns, column]
  return columns.length === 1 ? columns : columns.filter(c => c !== column)
}

/** Moves column to just before `before`, or to the right end when before is null. */
export function moveColumn(columns: string[], column: string, before: string | null): string[] {
  if (column === before || !columns.includes(column)) return columns
  const rest = columns.filter(c => c !== column)
  const at = before === null ? -1 : rest.indexOf(before)
  return at === -1 ? [...rest, column] : [...rest.slice(0, at), column, ...rest.slice(at)]
}

/** What the picker offers: the built-in fields, then the Attributes the Logs have, by name. */
export function pickableFields(logs: Log[]): string[] {
  const attributes = new Set<string>()
  for (const log of logs.slice(0, PICKER_LOGS)) {
    for (const name of Object.keys(log.attributes ?? {})) attributes.add(name)
  }
  return [...BUILT_IN_FIELDS, ...[...attributes].filter(name => !isBuiltIn(name)).sort()]
}

/**
 * The Logs the Builder suggests from after a run: the run's first Logs, then
 * those kept from earlier runs. A Query narrowed by a filter so still suggests
 * the values the filter left out.
 */
export function rememberLogs(kept: Log[], latest: Log[]): Log[] {
  const fresh = latest.slice(0, PICKER_LOGS)
  const ids = new Set(fresh.map(l => l.id))
  return [...fresh, ...kept.filter(l => !ids.has(l.id))].slice(0, SUGGEST_LOGS)
}

/** A Field the Builder suggests, with the type its values have in the Logs read. */
export interface FieldInfo {
  name: string
  /** string, number, bool, array, object or time; "mixed" when the Logs differ. */
  type: string
}

const BUILT_IN_TYPES: Record<BuiltInField, string> = {
  time: 'time', level: 'string', service: 'string', environment: 'string', message: 'string', trace_id: 'string', span_id: 'string',
}

/** The built-in fields, then the Attributes the Logs have by name, each with its type. */
export function fieldInfos(logs: Log[]): FieldInfo[] {
  const types = new Map<string, string>()
  for (const log of logs) {
    for (const [name, value] of Object.entries(log.attributes ?? {})) {
      if (isBuiltIn(name) || value === null || value === undefined) continue
      const type = typeOf(value)
      const seen = types.get(name)
      types.set(name, seen === undefined || seen === type ? type : 'mixed')
    }
  }
  const attributes = [...types.keys()].sort().map(name => ({ name, type: types.get(name)! }))
  return [...BUILT_IN_FIELDS.map(name => ({ name, type: BUILT_IN_TYPES[name] })), ...attributes]
}

/** The values a Field has most often in the Logs read, strings, numbers and bools only. */
export function commonValues(logs: Log[], field: string, limit = 8): (string | number | boolean)[] {
  const counts = new Map<string, { value: string | number | boolean; n: number }>()
  for (const log of logs) {
    const value = columnValue(log, field)
    if (typeof value !== 'string' && typeof value !== 'number' && typeof value !== 'boolean') continue
    if (typeof value === 'string' && (value === '' || value.length > 200)) continue
    const key = `${typeof value}:${value}`
    const entry = counts.get(key)
    if (entry) entry.n++
    else counts.set(key, { value, n: 1 })
  }
  return [...counts.values()].sort((a, b) => b.n - a.n).slice(0, limit).map(e => e.value)
}

function typeOf(value: AttributeValue): string {
  if (Array.isArray(value)) return 'array'
  if (typeof value === 'boolean') return 'bool'
  return typeof value
}

function isBuiltIn(column: string): column is BuiltInField {
  return (BUILT_IN_FIELDS as readonly string[]).includes(column)
}

function sameColumns(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((c, i) => c === b[i])
}

function unique(columns: string[]): string[] {
  return [...new Set(columns)]
}
