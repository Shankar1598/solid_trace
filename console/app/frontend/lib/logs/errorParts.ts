// A run's error as the result area shows it. DuckDB points at a mistake in
// SQL with the line it is on and a caret under it:
//
//   LINE 1: SELECT nope FROM logs
//                  ^
//
// Those two lines keep their columns, or the caret points at nothing, while
// the prose around them wraps to fit a phone.

/** Some of an error's lines: prose to wrap, or a LINE and its caret to keep as written. */
export type ErrorPart = { kind: 'prose' | 'pointer'; text: string }

const LINE = /^LINE \d+: /
const CARET = /^\s*\^\s*$/

export function errorParts(message: string): ErrorPart[] {
  const lines = message.split('\n')
  const parts: ErrorPart[] = []
  let prose: string[] = []
  const endProse = () => {
    // Blank lines between parts become the space between them.
    while (prose.length > 0 && prose[0].trim() === '') prose.shift()
    while (prose.length > 0 && prose[prose.length - 1].trim() === '') prose.pop()
    if (prose.length > 0) parts.push({ kind: 'prose', text: prose.join('\n') })
    prose = []
  }
  for (let i = 0; i < lines.length; i++) {
    if (LINE.test(lines[i]) && CARET.test(lines[i + 1] ?? '')) {
      endProse()
      parts.push({ kind: 'pointer', text: `${lines[i]}\n${lines[i + 1].trimEnd()}` })
      i++
    } else {
      prose.push(lines[i])
    }
  }
  endProse()
  return parts
}
