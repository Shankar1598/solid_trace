import { describe, expect, it } from 'vitest'
import { addOlderPage, startStream, streamEnd, type Log, type LogPage } from './stream'

function page(first: number, count: number, nextCursor: string | null): LogPage {
  const logs: Log[] = Array.from({ length: count }, (_, i) => ({
    id: `log-${first + i}`,
    time: '2026-10-03T10:00:00Z',
    message: `message ${first + i}`,
    attributes: {},
  }))
  return { logs, next_cursor: nextCursor }
}

describe('the stream of Logs', () => {
  it('adds older pages after the newer ones', () => {
    const stream = addOlderPage(startStream(page(0, 200, 'c1')), page(200, 200, 'c2'))

    expect(stream.logs).toHaveLength(400)
    expect(stream.logs[0].id).toBe('log-0')
    expect(stream.logs[399].id).toBe('log-399')
    expect(stream.nextCursor).toBe('c2')
    expect(streamEnd(stream)).toBe('more')
  })

  it('ends when EventStore has no older Logs', () => {
    expect(streamEnd(startStream(page(0, 37, null)))).toBe('end')
  })

  it('stops at 10,000 Logs and asks to narrow the range', () => {
    let stream = startStream(page(0, 200, 'c'))
    while (streamEnd(stream) === 'more') stream = addOlderPage(stream, page(stream.logs.length, 200, 'c'))

    expect(stream.logs).toHaveLength(10_000)
    expect(streamEnd(stream)).toBe('capped')
  })

  it('ends rather than asking to narrow when the 10,000th Log is the oldest', () => {
    let stream = startStream(page(0, 200, 'c'))
    while (stream.logs.length < 9_800) stream = addOlderPage(stream, page(stream.logs.length, 200, 'c'))
    stream = addOlderPage(stream, page(9_800, 200, null))

    expect(streamEnd(stream)).toBe('end')
  })
})
