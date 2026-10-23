// A run's error as the result area shows it. ClickHouse says where SQL fails
// to parse as a position, counted in bytes from 1:
//
//   Syntax error: failed at position 31 (level): level = 'error'. Expected one of: …
//
// With the SQL that ran, the error also shows the line of the SQL that holds
// the position, and a caret under it:
//
//   LINE 1: SELECT message FROM logs WHER level = 'error'
//                                         ^
//
// Those two lines keep their columns, or the caret points at nothing, while
// the prose above them wraps to fit a phone.

/** Some of an error's lines: prose to wrap, or a LINE and its caret to keep as written. */
export type ErrorPart = { kind: 'prose' | 'pointer'; text: string }

const POSITION = /\bfailed at position (\d+)\b/

export function errorParts(message: string, sql?: string): ErrorPart[] {
  const prose: ErrorPart = { kind: 'prose', text: message }
  const position = POSITION.exec(message)
  if (!position || sql === undefined) return [prose]
  const at = charIndex(sql, Number(position[1]) - 1)
  if (at === null) return [prose]

  const lines = sql.slice(0, at).split('\n')
  const number = lines.length
  const before = lines[number - 1]
  const line = before + sql.slice(at).split('\n')[0]
  const label = `LINE ${number}: `
  // Tabs stay tabs, so the caret lines up however wide they are drawn.
  const indent = ' '.repeat(label.length) + before.replace(/[^\t]/g, ' ')
  return [prose, { kind: 'pointer', text: `${label}${line}\n${indent}^` }]
}

/** The index in `text` of the character that starts at UTF-8 byte `offset`; its length at the end; null past it. */
function charIndex(text: string, offset: number): number | null {
  const encoder = new TextEncoder()
  let bytes = 0
  let index = 0
  for (const char of text) {
    if (bytes >= offset) break
    bytes += encoder.encode(char).length
    index += char.length
  }
  return bytes === offset ? index : null
}
