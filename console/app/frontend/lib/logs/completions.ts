// What the QueryEditor offers as a member types a Query: stages at the start
// and after each |, Fields with their types and functions where an
// expression goes (aggregates in summarize), by and orders where a stage
// takes them, operators after a Field, and a Field's recent values after a
// comparison. It reads only the text before the cursor, with the Fields of
// recent Logs the Builder suggests; the editor narrows the options as more is
// typed.
import { nameText, valueText, type ChipValue } from './builder'
import type { FieldInfo } from './columns'
import { AGGREGATES, LOGICAL, OPERATORS, SCALAR_FUNCTIONS, STAGES, type FunctionInfo } from './language'
import { fieldOptions } from './pick'
import { tokenize, type Token } from './queryLexer'

export interface CompletionSources {
  /** The Fields of recent Logs, with their types: what the Builder suggests. */
  fields: FieldInfo[]
  /** The values a Field has most often in recent Logs. */
  valuesOf?: (field: string) => ChipValue[]
  /** The Query's source, such as `['my-api']`, offered at the start. */
  source?: string
}

export type CompletionType = 'source' | 'stage' | 'field' | 'function' | 'operator' | 'keyword' | 'value'

export interface QueryCompletion {
  label: string
  type: CompletionType
  /** After the label: a Field's or value's type, what a stage or function takes, as `(x, p)`. */
  detail?: string
  /** What it does, in a line. */
  info?: string
  /** Put in place of the word typed; the label when absent. */
  apply?: string
  /** Where the cursor lands within apply; its end when absent. */
  cursor?: number
  /** Offer completions again once applied, as after a stage's name. */
  reopen?: boolean
  /** The heading it is listed under. */
  section?: string
}

/** Options for the text from `from` to the cursor, which they replace. */
export interface Completions {
  from: number
  options: QueryCompletion[]
}

/**
 * The completions at `pos` in `text`. Mid-expression, with nothing typed,
 * there are none unless `explicit` (Ctrl+Space), so a space doesn't open a
 * list of every Field.
 */
export function completeQuery(text: string, pos: number, sources: CompletionSources, explicit = false): Completions | null {
  const before = text.slice(0, pos)
  const tokens = tokenize(before)
  const last = tokens.at(-1)

  if (last && last.to === pos && last.kind === 'comment') return null
  // Within a string: only a compared value, quote and all.
  if (last && last.to === pos && last.kind === 'string' && !closed(last.text)) {
    return values(tokens.slice(0, -1), last.from, sources)
  }

  const word = typedWord(before)
  const from = pos - word.length
  const context = tokens.filter(t => t.to <= from)
  const pipe = context.map(t => t.kind).lastIndexOf('pipe')
  const stage = context.slice(pipe + 1)
  const typed = word !== '' || explicit

  if (stage.length === 0) {
    const options = stageOptions(before.slice(0, from))
    if (pipe < 0 && sources.source) options.unshift({ label: sources.source, type: 'source', detail: 'this Project', apply: `${sources.source}\n| `, reopen: true })
    return { from, options }
  }

  const head = stage[0]
  if (head.kind !== 'stage') return null
  const rest = stage.slice(1)
  const prev = rest.at(-1)
  const wordAt = (options: QueryCompletion[] | null) => (options && options.length > 0 ? { from, options } : null)

  switch (head.text) {
    case 'where':
      return wordAt(condition(rest, from, sources, typed))
    case 'summarize': {
      const by = rest.findIndex(t => t.kind === 'keyword' && t.text === 'by' && depth(rest.slice(0, rest.indexOf(t))) === 0)
      if (by < 0) {
        if (prev && operandEnds(prev) && depth(rest) === 0) return wordAt([keyword('by', 'Groups the rows by these expressions.')])
        if (depth(rest) > 0) return wordAt(prev && operandEnds(prev) ? null : operands(sources, { typed: typed || prev?.text === '(' }))
        return wordAt(typed || !prev || prev.text === ',' ? [...AGGREGATES.map(f => fn(f, 'Aggregates')), keyword('by', 'Groups the rows by these expressions.')] : null)
      }
      if (prev && operandEnds(prev)) return null
      return wordAt(operands(sources, { typed: typed || prev?.text === 'by', bins: true }))
    }
    case 'sort':
    case 'order':
    case 'top': {
      const by = rest.findIndex(t => t.kind === 'keyword' && t.text === 'by')
      if (by < 0) {
        const ready = head.text === 'top' ? rest.length === 1 && rest[0].kind === 'number' : rest.length === 0
        return wordAt(ready ? [keyword('by', 'Sorts by these expressions.')] : null)
      }
      if (prev && operandEnds(prev)) return wordAt(SORT_ORDERS)
      return wordAt(operands(sources, { typed: typed || prev?.text === 'by', space: true }))
    }
    case 'project':
    case 'extend':
    case 'distinct':
      if (prev && operandEnds(prev)) return null
      return wordAt(operands(sources, { typed: typed || !prev }))
    default:
      // search, take, limit, count: nothing to offer.
      return null
  }
}

/** A condition: Fields to start it, operators after a Field, values after an operator, and or or after it all. */
function condition(rest: Token[], from: number, sources: CompletionSources, typed: boolean): QueryCompletion[] | null {
  const prev = rest.at(-1)
  const compared = comparedField(rest)
  if (compared) return values(rest, from, sources)?.options ?? (typed ? operands(sources, { typed, space: true }) : null)
  if (prev && operandEnds(prev)) {
    const value = prev.kind !== 'field' && prev.kind !== 'name'
    return value ? LOGICAL.map(l => ({ label: l.name, type: 'operator', info: l.info, apply: `${l.name} `, reopen: true })) : OPERATOR_OPTIONS
  }
  return operands(sources, { typed: typed || !prev || prev.kind === 'operator' || prev.text === '(', space: true })
}

/** Fields, then functions (time bins first when `bins`); none until something is typed. */
function operands(sources: CompletionSources, { typed, bins = false, space = false }: { typed: boolean; bins?: boolean; space?: boolean }): QueryCompletion[] | null {
  if (!typed) return null
  const time = sources.fields.filter(f => f.name === 'time')
  const fields: QueryCompletion[] = [
    ...time.map(f => ({ value: f.name, hint: f.type, group: 'Built-in' })),
    ...fieldOptions(sources.fields),
  ].map(o => ({
    label: o.value,
    type: 'field',
    detail: o.hint,
    apply: `${nameText(o.value)}${space ? ' ' : ''}`,
    reopen: space,
    section: 'Fields',
  }))
  const functions = SCALAR_FUNCTIONS.map(f => fn(f, 'Functions'))
  if (bins) {
    const first = ['bin_auto', 'bin']
    const binFns = first.map(name => functions.find(f => f.label === name)!).map(f => ({ ...f, section: 'Time bins' }))
    return [...binFns, ...fields, ...functions.filter(f => !first.includes(f.label))]
  }
  return [...fields, ...functions]
}

/** The values of the Field a comparison names, when the cursor is where its value goes. */
function values(context: Token[], from: number, sources: CompletionSources): Completions | null {
  const compared = comparedField(context)
  if (!compared || !sources.valuesOf) return null
  const textOnly = /contains|startswith|matches/.test(compared.operator)
  const options: QueryCompletion[] = sources.valuesOf(compared.field)
    .filter(v => !textOnly || typeof v === 'string')
    .map(v => ({ label: valueText(v), type: 'value', detail: typeof v === 'boolean' ? 'bool' : typeof v, section: 'Recent values' }))
  return options.length > 0 ? { from, options } : null
}

/**
 * The Field and operator a value is being written for: `level ==`, or a
 * list after `in (` as far as its last comma. Null elsewhere.
 */
function comparedField(context: Token[]): { field: string; operator: string } | null {
  let end = context.length - 1
  if (context[end]?.text === '(' || context[end]?.text === ',') {
    // Back to the list's opening parenthesis.
    let open = end
    let level = 0
    for (; open >= 0; open--) {
      const text = context[open].text
      if (text === ')') level++
      else if (text === '(' && level-- === 0) break
    }
    if (open < 1 || !/^!?in$/.test(context[open - 1].text)) return null
    end = open - 1
  }
  const operator = context[end]
  const field = context[end - 1]
  if (!operator || operator.kind !== 'operator' || /^(and|or)$/.test(operator.text) || /^[-+*/%]$/.test(operator.text) || operator.text === '=') return null
  if (!field || (field.kind !== 'field' && field.kind !== 'name')) return null
  return { field: field.kind === 'name' ? unbracket(field.text) : fieldName(context, end - 1), operator: operator.text }
}

/** A dotted Field's whole name, ending at token `i`: http.method. */
function fieldName(context: Token[], i: number): string {
  let name = context[i].text
  while (i >= 2 && context[i - 1].text === '.' && context[i - 1].from === context[i - 2].to && context[i - 2].kind === 'field') {
    name = `${context[i - 2].text}.${name}`
    i -= 2
  }
  return name
}

function unbracket(text: string): string {
  return text.slice(2, -2).replace(/\\(.)/g, '$1')
}

function stageOptions(textBefore: string): QueryCompletion[] {
  const space = textBefore.endsWith('|') ? ' ' : ''
  const main = STAGES.filter(s => !s.alias)
  const aliases = STAGES.filter(s => s.alias)
  return [...main, ...aliases].map(s => {
    const takesMore = s.name !== 'count'
    return {
      label: s.name,
      type: 'stage',
      detail: s.signature.slice(s.name.length + 1),
      info: s.info,
      apply: `${space}${s.name}${takesMore ? ' ' : ''}`,
      reopen: takesMore,
    }
  })
}

/**
 * A function, applied with its parentheses and the cursor between them when
 * it takes arguments, which completions then offer; or as its `write` says.
 */
function fn(f: FunctionInfo, section: string): QueryCompletion {
  const takesArgs = f.args !== ''
  const base = { label: f.name, type: 'function' as const, detail: `(${f.args})`, info: f.info, section }
  if (f.write) return { ...base, apply: f.write.replace('|', ''), cursor: f.write.indexOf('|') }
  return { ...base, apply: `${f.name}()`, cursor: takesArgs ? f.name.length + 1 : undefined, reopen: takesArgs }
}

function keyword(name: string, info: string): QueryCompletion {
  return { label: name, type: 'keyword', info, apply: `${name} `, reopen: true }
}

const OPERATOR_OPTIONS: QueryCompletion[] = OPERATORS.map(o => ({
  label: o.name, type: 'operator', info: o.info, apply: `${o.name} `, reopen: true,
}))

const SORT_ORDERS: QueryCompletion[] = [
  { label: 'desc', type: 'keyword', info: 'Largest or newest first, missing values last.' },
  { label: 'asc', type: 'keyword', info: 'Smallest or oldest first, missing values first.' },
  { label: 'nulls first', type: 'keyword', info: 'Missing values first.' },
  { label: 'nulls last', type: 'keyword', info: 'Missing values last.' },
]

/** Whether a token ends an operand, so an operator or keyword comes next. */
function operandEnds(token: Token): boolean {
  return ['field', 'name', 'string', 'number', 'timespan', 'literal'].includes(token.kind) || token.text === ')'
}

/** How many parentheses are open at the end of `tokens`. */
function depth(tokens: Token[]): number {
  let open = 0
  for (const t of tokens) {
    if (t.text === '(') open++
    else if (t.text === ')') open = Math.max(0, open - 1)
  }
  return open
}

/**
 * The word being typed before the cursor: a name (dotted), an operator word
 * with its !, or the symbols of an operator such as == or !.
 */
function typedWord(before: string): string {
  return /!?[\p{L}_$][\p{L}\p{N}_$.]*$/u.exec(before)?.[0] ?? /[=!<>~]+$/.exec(before)?.[0] ?? ''
}

/** Whether a string token has its closing quote. */
function closed(text: string): boolean {
  if (text.length < 2 || text.at(-1) !== text[0]) return false
  let escapes = 0
  for (let i = text.length - 2; i > 0 && text[i] === '\\'; i--) escapes++
  return escapes % 2 === 0
}
