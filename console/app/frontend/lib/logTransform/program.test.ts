import { describe, expect, it } from 'vitest'
import { failuresHref, marksOf, type ProgramProblem } from './program'

const problem = (start: number, end: number, message = 'x'): ProgramProblem =>
  ({ message, code: 100, start, end, text: `error[E100]: ${message}` })

const marked = (program: string, p: ProgramProblem) => {
  const [mark] = marksOf(program, { errors: [p], warnings: [] })
  return program.slice(mark.from, mark.to)
}

describe("where the check's errors and warnings sit", () => {
  it('marks the byte span the check gives', () => {
    expect(marked('.a = nope(1)', problem(5, 9))).toBe('nope')
  })

  it('counts bytes, so text after non-ASCII characters is marked where it is', () => {
    // "é" is two bytes, "😀" four, and "日本" six: the check counts them so.
    const program = '.a = "é😀日本" + nope'
    const start = new TextEncoder().encode('.a = "é😀日本" + ').length
    expect(start).toBe(22)
    expect(marked(program, problem(22, 26))).toBe('nope')
    expect(marked(program, problem(6, 18))).toBe('é😀日本')
  })

  it('marks one character for a span at a point, or nothing past the end', () => {
    expect(marked('.a = ', problem(3, 3))).toBe('=')
    expect(marked('.a =', problem(4, 4))).toBe('')
    expect(marked('.a', problem(10, 12))).toBe('')
  })

  it("gives errors and warnings their severity and VRL's message", () => {
    const marks = marksOf('.a = 1\n.b = 2', { errors: [problem(0, 2, 'bad')], warnings: [problem(7, 9, 'unused')] })
    expect(marks).toEqual([
      { from: 0, to: 2, severity: 'error', message: 'bad' },
      { from: 7, to: 9, severity: 'warning', message: 'unused' },
    ])
  })
})

describe('the failed count', () => {
  it("links to the explorer with the Project's Logs the program failed on", () => {
    const href = failuresHref('acme', 'shop')
    const url = new URL(href, 'http://console')
    expect(url.pathname).toBe('/acme/projects/shop/logs')
    expect(url.searchParams.get('q')).toBe("['shop']\n| where isnotnull(solidtrace.transform_error)")
    expect(url.searchParams.get('range')).toBe('24h')
  })
})
