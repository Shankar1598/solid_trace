import { describe, expect, it } from 'vitest'
import { elapsedOf, runLine } from './runLine'

describe('the run line', () => {
  it('counts Logs or rows, and says how long the run took', () => {
    expect(runLine(1415, 'Log', 230)).toBe('1,415 Logs · 230 ms')
    expect(runLine(1, 'Log', 0)).toBe('1 Log · 0 ms')
    expect(runLine(12, 'row', 1430)).toBe('12 rows · 1.4 s')
    expect(runLine(1, 'row', 61_000)).toBe('1 row · 61 s')
  })

  it('leaves the time out when the answer had none', () => {
    expect(runLine(3, 'row', null)).toBe('3 rows')
  })

  it('reads elapsed_ms from an answer, and nothing else', () => {
    expect(elapsedOf({ logs: [], elapsed_ms: 230 })).toBe(230)
    expect(elapsedOf({ logs: [] })).toBeNull()
    expect(elapsedOf({ elapsed_ms: '230' })).toBeNull()
    expect(elapsedOf(null)).toBeNull()
  })
})
