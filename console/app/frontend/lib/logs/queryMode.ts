// Query mode: the Query as editable text. Leaving it for the Builder and
// pivoting from its results both go by what validate says of the text (its
// chips, and where each stage is), so the browser never parses a Query.
import type { QueryProblem, QueryStage, Validation } from './api'
import { builderOf, filterText, type Builder, type Chip } from './builder'

/**
 * Query → Builder: switch silently when the Builder can show the Query;
 * otherwise ask, naming the stages it can't show (or the Query's mistakes).
 */
export type ToBuilder =
  | { kind: 'switch'; builder: Builder }
  | { kind: 'ask'; stages: string[]; problems: QueryProblem[] }

export function toBuilder(query: string, validation: Validation): ToBuilder {
  if (validation.chips) return { kind: 'switch', builder: builderOf(validation.chips) }
  return {
    kind: 'ask',
    stages: validation.stages.filter(s => !s.builder).map(s => textOf(query, s)),
    problems: validation.errors,
  }
}

/**
 * Show only this or Hide this in Query mode: a where stage after the leading
 * filters, so it narrows the Logs that feed any summarize after them.
 */
export function pivotQuery(query: string, stages: QueryStage[], chip: Chip): string {
  const where = filterText(chip)
  const filters = stages.filter(s => s.filter)
  if (filters.length > 0) {
    const at = offsetOf(query, filters[filters.length - 1].end_line, filters[filters.length - 1].end_column)
    return `${query.slice(0, at)}\n| ${where}${query.slice(at)}`
  }
  if (stages.length > 0) {
    const at = offsetOf(query, stages[0].line, stages[0].column)
    return `${query.slice(0, at)}${where}\n| ${query.slice(at)}`
  }
  // No stages: an empty Query, or only the leading logs.
  return query.trim() === '' ? where : `${query.trimEnd()}\n| ${where}`
}

function textOf(query: string, stage: QueryStage): string {
  return query.slice(offsetOf(query, stage.line, stage.column), offsetOf(query, stage.end_line, stage.end_column))
}

/** The string index of a line and column, columns counted in characters as validate does. */
function offsetOf(text: string, line: number, column: number): number {
  let l = 1
  let c = 1
  let at = 0
  for (const char of text) {
    if (l === line && c === column) return at
    if (char === '\n') {
      l++
      c = 1
    } else {
      c++
    }
    at += char.length
  }
  return at
}
