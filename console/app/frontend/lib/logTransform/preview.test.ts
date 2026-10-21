import { describe, expect, it } from 'vitest'
import { pastedLogs } from './preview'

describe('Logs pasted to preview on', () => {
  it('takes a JSON array of objects', () => {
    expect(pastedLogs('[{"message": "one", "tags": ["a", null]}, {"message": "two", "n": 1.5}]')).toEqual({
      logs: [{ message: 'one', tags: ['a', null] }, { message: 'two', n: 1.5 }],
    })
  })

  it('takes one object as one Log', () => {
    expect(pastedLogs('  {"message": "GET /health 200", "service": "web"}\n')).toEqual({
      logs: [{ message: 'GET /health 200', service: 'web' }],
    })
  })

  it("says what's wrong with text that isn't JSON", () => {
    const parsed = pastedLogs('{"message": "one",}')
    expect(parsed).toHaveProperty('error')
    expect('error' in parsed && parsed.error).toMatch(/^The pasted Logs aren't JSON: /)
  })

  it('says which Log in an array is not an object', () => {
    expect(pastedLogs('[{"message": "one"}, "two"]')).toEqual({ error: "Log 2 isn't a JSON object." })
    expect(pastedLogs('[null]')).toEqual({ error: "Log 1 isn't a JSON object." })
    expect(pastedLogs('[[{"message": "one"}]]')).toEqual({ error: "Log 1 isn't a JSON object." })
  })

  it('asks for an object or an array of them, and for at least one Log', () => {
    expect(pastedLogs('"one"')).toEqual({ error: 'Paste a JSON object, or an array of them.' })
    expect(pastedLogs('[]')).toEqual({ error: 'Paste at least one Log.' })
    expect(pastedLogs('   ')).toEqual({ error: 'Paste at least one Log.' })
  })
})
