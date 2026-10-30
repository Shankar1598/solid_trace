import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchHistogram, fetchLogPage, fetchRun } from './api'

// The explorer's runs through Console's proxy of EventStore, with fetch
// standing in for Console: what each answer becomes, and what a run asks.

const range = { orgSlug: 'acme', projectSlug: 'shop', from: new Date('2026-10-23T09:00:00Z'), to: new Date('2026-10-30T09:00:00Z') }

/** EventStore's refusal of a search over its budget, as Console passes it on. */
const refusal = {
  message: 'Not run: this Query matches text (search, or contains, startswith or =~ on message), so it reads the text of every Log '
    + 'that its filters on service and file keep: about 12.4 GiB in 31.2M Logs from 2026-10-23T09:00:00Z to 2026-10-30T09:00:00Z, '
    + 'past the 4 GiB a Query that matches text may read.',
  estimate: {
    from: '2026-10-23T09:00:00Z', to: '2026-10-30T09:00:00Z', text_match: true, logs: 31_200_000, bytes: 13_314_398_617,
    budget_bytes: 4_294_967_296, over_budget: true, heavy: true, forced: false,
  },
  largest_range: { from: '2026-10-28T07:12:00Z', to: '2026-10-30T09:00:00Z', bytes: 4_290_000_000 },
  top_files: [
    { service: 'indexer', file: 'production.log', logs: 20_100_000, bytes: 9_100_000_000 },
    { service: 'web', file: '', logs: 3_000_000, bytes: 900_000_000 },
  ],
  narrower_query: { query: "['shop']\n| where file == \"production.log\"\n| search \"boom\"", from: '2026-10-23T09:00:00Z', to: '2026-10-30T09:00:00Z' },
}

/** Console answering every request with `body`; the URLs asked for. */
function answer(body: unknown, status = 200): string[] {
  const urls: string[] = []
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    urls.push(url)
    return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
  }))
  return urls
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('a run of the explorer', () => {
  it('answers a refusal of a Query that reads too much, not an error', async () => {
    answer({ refusal, elapsed_ms: 3 })

    expect(await fetchRun({ ...range, query: 'search "boom"' })).toEqual({ kind: 'refused', refusal })
  })

  it('runs the Query as written when forced, as Run anyway does, and is checked otherwise', async () => {
    const urls = answer({ logs: [], next_cursor: null })

    await fetchRun({ ...range, query: 'search "boom"', force: true })
    await fetchRun({ ...range, query: 'search "boom"' })

    expect(new URL(urls[0], 'http://console').searchParams.get('force')).toBe('1')
    expect(new URL(urls[1], 'http://console').searchParams.has('force')).toBe(false)
  })

  it("says why where only a page of Logs will do, as the refusal's message", async () => {
    answer({ refusal: { message: 'Another large Query is running. Run them one at a time.', busy: true } })

    await expect(fetchLogPage({ ...range, query: 'search "boom"' })).rejects.toThrow('Another large Query is running. Run them one at a time.')
  })
})

describe("the explorer's histogram", () => {
  const counted = { bucket_seconds: 3600, buckets: [{ time: '2026-10-30T08:00:00Z', count: 2, levels: { info: 2 } }], count: 2, levels: { info: 2 } }

  it('answers its counts, or the refusal its Query got', async () => {
    answer(counted)
    expect(await fetchHistogram({ ...range, query: 'search "boom"', bucketSeconds: 3600 })).toEqual({ kind: 'counts', histogram: counted })

    answer({ refusal, elapsed_ms: 3 })
    expect(await fetchHistogram({ ...range, query: 'search "boom"', bucketSeconds: 3600 })).toEqual({ kind: 'refused', refusal })
  })

  it('counts as written when its Query is forced', async () => {
    const urls = answer(counted)

    await fetchHistogram({ ...range, query: 'search "boom"', bucketSeconds: 3600, force: true })
    await fetchHistogram({ ...range, query: 'search "boom"', bucketSeconds: 3600 })

    expect(new URL(urls[0], 'http://console').searchParams.get('force')).toBe('1')
    expect(new URL(urls[1], 'http://console').searchParams.has('force')).toBe(false)
  })
})
