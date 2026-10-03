import { describe, expect, it } from 'vitest'
import { addOlderPage, fillStream, startStream, streamEnd, wantOlder, type Log, type LogPage } from './stream'

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

describe('filling the stream while EventStore answers short', () => {
  it('fetches on until it has a page of Logs, showing each answer', async () => {
    // EventStore answers after its scan budget with what it found.
    const answers = [page(3, 0, 'c2'), page(3, 5, 'c3'), page(8, 200, 'c4')]
    const cursors: Array<string | null> = []
    const shown: number[] = []

    const stream = await fillStream(startStream(page(0, 3, 'c1')), async cursor => {
      cursors.push(cursor)
      return answers.shift()!
    }, s => shown.push(s.logs.length))

    expect(cursors).toEqual(['c1', 'c2', 'c3'])
    expect(shown).toEqual([3, 8, 208])
    expect(stream.logs).toHaveLength(208)
    expect(stream.nextCursor).toBe('c4')
  })

  it('stops when there are no older Logs', async () => {
    const stream = await fillStream(startStream(page(0, 0, 'c1')), async () => page(0, 2, null), () => {})

    expect(stream.logs).toHaveLength(2)
    expect(streamEnd(stream)).toBe('end')
  })

  it("doesn't fetch a stream that already has its page", async () => {
    let fetched = 0
    await fillStream(startStream(page(0, 200, 'c1')), async () => { fetched++; return page(200, 200, null) }, () => {})

    expect(fetched).toBe(0)
  })

  it('asks for one page more when the reader scrolls to the end', async () => {
    const stream = await fillStream(wantOlder(startStream(page(0, 200, 'c1'))), async cursor =>
      cursor === 'c1' ? page(200, 50, 'c2') : page(250, 200, 'c3'), () => {})

    expect(stream.logs).toHaveLength(450)
  })

  it('stops at 10,000 Logs', async () => {
    let stream = startStream(page(0, 200, 'c'))
    while (stream.logs.length < 9_900) stream = addOlderPage(stream, page(stream.logs.length, 200, 'c'))
    stream = await fillStream(wantOlder(stream), async () => page(0, 50, 'c'), () => {})

    expect(stream.logs).toHaveLength(10_000)
    expect(streamEnd(stream)).toBe('capped')
  })

  it('stops when cancelled', async () => {
    let fetched = 0
    // A cancelled run's fetch rejects.
    const filling = fillStream(startStream(page(0, 0, 'c1')), async () => {
      fetched++
      throw new DOMException('Aborted', 'AbortError')
    }, () => {})

    await expect(filling).rejects.toThrow('Aborted')
    expect(fetched).toBe(1)
  })
})
