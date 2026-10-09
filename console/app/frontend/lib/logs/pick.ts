// The Builder's pick lists: Fields, functions and values, narrowed as the
// reader types, then picked with a click or the arrows and Enter.
import { editableText, type ChipValue } from './builder'
import { BUILT_IN_FIELDS, type FieldInfo } from './columns'

/** One choice in a pick list. */
export interface PickOption {
  /** What picking it gives; matched against what's typed unless text is set. */
  value: string
  /** Shown in place of the value. */
  label?: string
  /** Matched against what's typed in place of the value. */
  text?: string
  /** Shown on the right, such as a Field's type. */
  hint?: string
  /** The heading it sits under; a group's options come together. */
  group?: string
  disabled?: boolean
}

/**
 * The options containing the typed text, ignoring case: within each group,
 * those starting with it first. Groups keep their order.
 */
export function narrow<T extends PickOption>(options: T[], typed: string): T[] {
  const t = typed.trim().toLowerCase()
  if (t === '') return options
  const groups = [...new Set(options.map(o => o.group))]
  return options
    .map((o, i) => ({ o, i, text: (o.text ?? o.value).toLowerCase() }))
    .filter(e => e.text.includes(t))
    .sort((a, b) =>
      groups.indexOf(a.o.group) - groups.indexOf(b.o.group) ||
      Number(!a.text.startsWith(t)) - Number(!b.text.startsWith(t)) ||
      a.i - b.i)
    .map(e => e.o)
}

/**
 * Fields to pick, all but time (the range filters on it, and bins group by
 * it): the built-in fields then the Attributes, or those of the preferred
 * types first.
 */
export function fieldOptions(fields: FieldInfo[], prefer?: { types: string[]; group: string }): PickOption[] {
  const named = fields.filter(f => f.name !== 'time')
  const option = (f: FieldInfo, group: string): PickOption => ({ value: f.name, hint: f.type, group })
  if (prefer) {
    return [
      ...named.filter(f => prefer.types.includes(f.type)).map(f => option(f, prefer.group)),
      ...named.filter(f => !prefer.types.includes(f.type)).map(f => option(f, 'Other Fields')),
    ]
  }
  const builtIn = BUILT_IN_FIELDS as readonly string[]
  return named.map(f => option(f, builtIn.includes(f.name) ? 'Built-in' : 'Attributes'))
}

/**
 * A Field's common values as the value box takes them: as typed for the text
 * operators, else so parseValue reads them back with their type.
 */
export function valueOptions(values: ChipValue[], asText: boolean): PickOption[] {
  return values.map(v => ({
    value: asText ? String(v) : editableText(v),
    hint: typeof v === 'boolean' ? 'bool' : typeof v,
  }))
}

/** A comma-separated list's values, blanks dropped. */
export function listParts(text: string): string[] {
  return text.split(',').map(p => p.trim()).filter(p => p !== '')
}

/** The part of a comma-separated list still being typed: its last, unless that's a known value. */
export function listFragment(text: string, known: string[]): string {
  if (text.trimEnd().endsWith(',')) return ''
  const last = listParts(text).at(-1) ?? ''
  return known.includes(last) ? '' : last
}

/**
 * The list with value added, or taken out when it's in it. A part still
 * being typed gives way to the value picked.
 */
export function toggleListValue(text: string, value: string, known: string[]): string {
  const parts = listParts(text)
  if (parts.includes(value)) return parts.filter(p => p !== value).join(', ')
  const fragment = listFragment(text, known)
  const kept = fragment === '' ? parts : parts.slice(0, -1)
  return [...kept, value].join(', ')
}
