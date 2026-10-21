// A Project's Log transform program as the page edits it: EventStore's check
// of a draft, its errors and warnings as the editor's underlines, and where
// the failed count links to.
import { withSource } from '../dashboards/dashboard'
import { searchOf } from '../logs/explorerState'

/** One of the check's errors or warnings: VRL's message and code, its span in bytes, and VRL's formatted diagnostic. */
export interface ProgramProblem {
  message: string
  code: number
  /** Byte offsets into the program (UTF-8), the end just past the span. */
  start: number
  end: number
  text: string
}

export interface ProgramCheck {
  errors: ProgramProblem[]
  warnings: ProgramProblem[]
}

/** An underline in the editor: offsets into the text as the editor counts (UTF-16), `[from, to)`. */
export interface ProgramMark {
  from: number
  to: number
  severity: 'error' | 'warning'
  message: string
}

/** The check's errors and warnings as underlines on `program`. A span at a point marks the character there. */
export function marksOf(program: string, check: ProgramCheck): ProgramMark[] {
  const mark = (severity: ProgramMark['severity']) => (p: ProgramProblem): ProgramMark => {
    const from = offsetOf(program, p.start)
    let to = Math.max(from, offsetOf(program, p.end))
    if (to === from && from < program.length) to = from + charLength(program, from)
    return { from, to, severity, message: p.message }
  }
  return [...check.errors.map(mark('error')), ...check.warnings.map(mark('warning'))]
}

/** The UTF-16 offset of the character at or after byte offset `bytes` (UTF-8), clamped to the text. */
function offsetOf(text: string, bytes: number): number {
  let offset = 0
  let counted = 0
  while (offset < text.length && counted < bytes) {
    const code = text.codePointAt(offset)!
    counted += code < 0x80 ? 1 : code < 0x800 ? 2 : code < 0x10000 ? 3 : 4
    offset += charLength(text, offset)
  }
  return offset
}

function charLength(text: string, offset: number): number {
  return text.codePointAt(offset)! > 0xffff ? 2 : 1
}

/** The Logs explorer over the last day of the Project's Logs that its program failed on. */
export function failuresHref(orgSlug: string, projectSlug: string): string {
  const query = withSource(`['']\n| where isnotnull(solidtrace.transform_error)`, projectSlug)
  const search = searchOf({ range: { kind: 'relative', span: '24h' }, mode: 'builder', query, sql: '', viz: 'auto' })
  return `/${orgSlug}/projects/${projectSlug}/logs${search}`
}
