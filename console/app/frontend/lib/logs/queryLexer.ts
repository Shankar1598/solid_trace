// The Query's tokens as the editor colours them and completions read them.
// It follows EventStore's scanner (logquery/parser/lex.go) closely enough to
// colour a Query, but never judges one: validate does that. Tokens never span
// lines, so the editor can read one line at a time, carrying LexState.
import { KEYWORDS, STAGE_WORDS, TIMESPAN_UNITS, WORD_OPERATORS } from './language'

export type TokenKind =
  | 'stage' // where, summarize, sort, … as a stage's first word
  | 'keyword' // by, asc, desc, nulls first
  | 'operator' // ==, contains, !in, and, …
  | 'function' // a name before (
  | 'name' // ['bracketed name'], and logs as a Query's source
  | 'field' // any other name
  | 'string'
  | 'number'
  | 'timespan' // 5m, 100ms
  | 'literal' // true, false, datetime(…)
  | 'comment' // // to the end of the line
  | 'pipe'
  | 'punctuation'
  | 'invalid'

export interface Token {
  kind: TokenKind
  from: number
  to: number
  text: string
}

/** What the lexer carries from one token to the next, and across lines. */
export interface LexState {
  /** The next word opens a stage: at the start, and after each |. */
  stageStart: boolean
  /** The last word read, for nulls first and last. */
  lastWord: string
}

export function startState(): LexState {
  return { stageStart: true, lastWord: '' }
}

const SYMBOLS = ['==', '!=', '=~', '!~', '<=', '>=', '<', '>', '=', '+', '-', '*', '/', '%']
const PUNCTUATION = new Set([',', '(', ')', '[', ']', '.'])
const IDENT_START = /[\p{L}_$]/u
const IDENT_PART = /[\p{L}\p{N}_$]/u

/**
 * Reads the token at `pos` in `line`: its kind (null for spaces) and where it
 * ends, always past `pos`. Updates `state`.
 */
export function readToken(line: string, pos: number, state: LexState): { kind: TokenKind | null; end: number } {
  const c = line[pos]
  if (/\s/.test(c)) {
    let end = pos + 1
    while (end < line.length && /\s/.test(line[end])) end++
    return { kind: null, end }
  }
  if (line.startsWith('//', pos)) return { kind: 'comment', end: line.length }

  const word = (kind: TokenKind, end: number) => {
    state.stageStart = false
    return { kind, end }
  }

  if (c === '|') {
    state.stageStart = true
    state.lastWord = ''
    return { kind: 'pipe', end: pos + 1 }
  }
  if (c === '"' || c === "'") return word('string', quoted(line, pos))
  if (line.startsWith("['", pos) || line.startsWith('["', pos)) {
    const close = quoted(line, pos + 1)
    return word('name', line[close] === ']' ? close + 1 : close)
  }
  if (/[0-9]/.test(c)) return word(...number(line, pos))
  if (IDENT_START.test(c)) return identifier(line, pos, state)
  // !contains, !in, !startswith_cs: one operator.
  if (c === '!' && IDENT_START.test(line[pos + 1] ?? '')) {
    const end = identEnd(line, pos + 1)
    if (WORD_OPERATORS.has(line.slice(pos + 1, end))) return word('operator', end)
  }
  const symbol = SYMBOLS.find(s => line.startsWith(s, pos))
  if (symbol) return word('operator', pos + symbol.length)
  if (PUNCTUATION.has(c)) return { kind: 'punctuation', end: pos + 1 }
  return word('invalid', pos + Math.max(1, String.fromCodePoint(line.codePointAt(pos)!).length))
}

/** The tokens of a whole Query, spaces left out, with offsets into `text`. */
export function tokenize(text: string): Token[] {
  const tokens: Token[] = []
  const state = startState()
  let offset = 0
  for (const line of text.split('\n')) {
    let pos = 0
    while (pos < line.length) {
      const { kind, end } = readToken(line, pos, state)
      if (kind) tokens.push({ kind, from: offset + pos, to: offset + end, text: line.slice(pos, end) })
      pos = end
    }
    offset += line.length + 1
  }
  return tokens
}

function identifier(line: string, pos: number, state: LexState): { kind: TokenKind; end: number } {
  const end = identEnd(line, pos)
  const name = line.slice(pos, end)
  const opensStage = state.stageStart
  const after = line.slice(end).trimStart()
  const lastWord = state.lastWord
  state.stageStart = false
  state.lastWord = name

  if (opensStage && STAGE_WORDS.has(name)) return { kind: 'stage', end }
  // logs as the Query's source, like ['my-api'].
  if (opensStage && name === 'logs') return { kind: 'name', end }
  if (name === 'datetime' && after.startsWith('(')) {
    const close = line.indexOf(')', end)
    return { kind: 'literal', end: close < 0 ? line.length : close + 1 }
  }
  // in ("a", "b") is an operator before a list, not a call.
  if (WORD_OPERATORS.has(name)) return { kind: 'operator', end }
  if (after.startsWith('(')) return { kind: 'function', end }
  if (name === 'true' || name === 'false') return { kind: 'literal', end }
  if (KEYWORDS.has(name) || (lastWord === 'nulls' && (name === 'first' || name === 'last'))) return { kind: 'keyword', end }
  return { kind: 'field', end }
}

function identEnd(line: string, pos: number): number {
  let end = pos
  while (end < line.length && IDENT_PART.test(line[end])) end++
  return end
}

/** Past the closing quote of the string opening at `pos`, or the end of the line. */
function quoted(line: string, pos: number): number {
  const quote = line[pos]
  for (let i = pos + 1; i < line.length; i++) {
    if (line[i] === '\\') i++
    else if (line[i] === quote) return i + 1
  }
  return line.length
}

/** A number, or a timespan when a unit follows it: 5m, 1.5h, 100ms. */
function number(line: string, pos: number): [TokenKind, number] {
  let end = pos
  const digits = () => { while (end < line.length && /[0-9]/.test(line[end])) end++ }
  digits()
  if (line[end] === '.' && /[0-9]/.test(line[end + 1] ?? '')) {
    end++
    digits()
  }
  let exponent = false
  if (/[eE]/.test(line[end] ?? '') && /^[+-]?[0-9]/.test(line.slice(end + 1, end + 3))) {
    exponent = true
    end++
    if (/[+-]/.test(line[end])) end++
    digits()
  }
  if (end < line.length && IDENT_PART.test(line[end])) {
    const unitEnd = identEnd(line, end)
    const timespan = !exponent && TIMESPAN_UNITS.has(line.slice(end, unitEnd))
    return [timespan ? 'timespan' : 'invalid', unitEnd]
  }
  return ['number', end]
}
