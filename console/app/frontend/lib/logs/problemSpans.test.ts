import { describe, expect, it } from 'vitest'
import { problemSpan } from './problemSpans'

const problem = (line: number, column: number, endLine = line, endColumn = column) =>
  ({ message: 'x', line, column, end_line: endLine, end_column: endColumn })

const marked = (text: string, p: ReturnType<typeof problem>) => {
  const { from, to } = problemSpan(text, p)
  return text.slice(from, to)
}

describe('where a mistake sits', () => {
  it('marks the span validate gives, lines and columns from 1', () => {
    const text = "['my-api']\n| where nope(level)"
    expect(marked(text, problem(2, 9, 2, 13))).toBe('nope')
  })

  it('counts columns in characters, as EventStore does', () => {
    const text = 'where m == "😀" and bad'
    // "😀" is one character but two UTF-16 units.
    expect(marked(text, problem(1, 20, 1, 23))).toBe('bad')
  })

  it('marks the word at a point', () => {
    expect(marked('where level =~~ 5', problem(1, 13))).toBe('=~~')
    expect(marked('where nope(x)', problem(1, 7))).toBe('nope')
  })

  it('marks the word before a point at the end of a line', () => {
    const text = "['my-api']\n| where level =="
    expect(marked(text, problem(2, 17))).toBe('==')
    expect(marked(`${text}\n`, problem(2, 17))).toBe('==')
  })

  it('stays within the text for positions past it', () => {
    expect(problemSpan('take', problem(9, 1))).toEqual({ from: 0, to: 4 })
    expect(problemSpan('', problem(1, 1))).toEqual({ from: 0, to: 0 })
  })
})
