// Where validate's mistakes sit in a Query's text, for the editor's
// squiggles. EventStore counts lines and columns from 1, columns in
// characters (code points), and ends a span just past its last character.
import type { QueryProblem } from './api'

/**
 * A problem's span as offsets into `text` (UTF-16, as the editor counts),
 * `[from, to)`, never empty: a mistake at a point marks the word there, or
 * at the end of a line the word before it, as in `where level ==`.
 */
export function problemSpan(text: string, problem: QueryProblem): { from: number; to: number } {
  const from = offsetOf(text, problem.line, problem.column)
  const to = Math.max(from, offsetOf(text, problem.end_line, problem.end_column))
  if (to > from) return { from, to }
  if (text === '') return { from: 0, to: 0 }

  if (from < text.length && !/\s/.test(text[from])) return { from, to: wordEnd(text, from) }
  // At a space or the end: the word before it.
  let end = from
  while (end > 0 && /\s/.test(text[end - 1])) end--
  if (end === 0) return { from, to: Math.min(text.length, from + 1) }
  return { from: wordStart(text, end), to: end }
}

/** The offset of a 1-based line and column, clamped to the text. */
function offsetOf(text: string, line: number, column: number): number {
  let offset = 0
  for (let l = 1; l < line; l++) {
    const next = text.indexOf('\n', offset)
    if (next < 0) return text.length
    offset = next + 1
  }
  const lineEnd = text.indexOf('\n', offset) < 0 ? text.length : text.indexOf('\n', offset)
  for (let c = 1; c < column && offset < lineEnd; c++) offset += text.codePointAt(offset)! > 0xffff ? 2 : 1
  return offset
}

const WORD = /[\p{L}\p{N}_$.]/u
const SYMBOL = /[=!<>~+\-*/%]/

function sameRun(a: string, b: string): boolean {
  return (WORD.test(a) && WORD.test(b)) || (SYMBOL.test(a) && SYMBOL.test(b))
}

function wordEnd(text: string, from: number): number {
  let end = from + 1
  while (end < text.length && sameRun(text[from], text[end])) end++
  return end
}

function wordStart(text: string, end: number): number {
  let start = end - 1
  while (start > 0 && sameRun(text[end - 1], text[start - 1])) start--
  return start
}
