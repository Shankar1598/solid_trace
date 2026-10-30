import { describe, expect, it } from 'vitest'
import { refusalView, type Refusal } from './refusal'

// What the explorer shows in place of results when EventStore refuses a run:
// the why, the estimate against the budget, ways to read less, and Run anyway.

/** A search over a week of a busy Project, refused for reading 12.3 GiB, as EventStore writes it. */
const overBudget: Refusal = {
  message: [
    'Not run: this Query matches text (search, or contains, startswith or =~ on message), so it reads the text of every Log that its '
      + 'filters on service and file keep: about 12.3 GiB in 31.2M Logs from 2026-10-23T09:00:00Z to 2026-10-30T09:00:00Z, past the '
      + '4 GiB a Query that matches text may read.',
    'The largest range that fits ends where this one does: the last 2d 1h, from 2026-10-28T07:12:00Z to 2026-10-30T09:00:00Z.',
    'The busiest files in the range, to filter on with == or in on service and file:',
    '- service "indexer", file "production.log": 20.1M Logs, 8.4 GiB',
    '- service "web", no file: 3M Logs, 858.3 MiB',
    'A narrower Query that fits, over the same range:',
    "['shop']\n| where file == \"production.log\"\n| search \"boom\"",
  ].join('\n'),
  busy: false,
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

describe('a refusal in the explorer', () => {
  it('says why, sets the estimate against the budget, and offers a range, the busiest files and Run anyway', () => {
    const view = refusalView(overBudget)

    // The rest of the message is the buttons below it.
    expect(view.message).toBe(
      'Not run: this Query matches text (search, or contains, startswith or =~ on message), so it reads the text of every Log that its '
        + 'filters on service and file keep: about 12.3 GiB in 31.2M Logs from 2026-10-23T09:00:00Z to 2026-10-30T09:00:00Z, past the '
        + '4 GiB a Query that matches text may read.')
    // Figures as EventStore writes them, to a tenth, so they agree with the message.
    expect(view.estimate).toEqual({ bytes: '12.3 GiB', logs: '31.2M Logs', budget: '4 GiB', budgetShare: expect.closeTo(0.3226, 3) })
    expect(view.range).toEqual({
      range: { kind: 'absolute', from: new Date('2026-10-28T07:12:00Z'), to: new Date('2026-10-30T09:00:00Z') },
      length: '2d 1h',
      bytes: '3.9 GiB',
    })
    expect(view.files).toEqual([
      { service: 'indexer', file: 'production.log', logs: '20.1M Logs', bytes: '8.4 GiB' },
      // Logs with no file can't be filtered on: file == "" misses them.
      { service: 'web', file: null, logs: '3M Logs', bytes: '858.3 MiB' },
    ])
    expect(view.runAnyway).toBe(true)
  })

  it('shows a refusal that has only a message as its message, with nothing empty under it', () => {
    const message = 'Not run: this Query reads too much.\nNarrow its range or filter on a file.'

    expect(refusalView({ message })).toEqual({ message, estimate: null, range: null, files: [], runAnyway: true })
  })

  it('shows only the message while another large Query runs, without Run anyway, which would be refused the same way', () => {
    const busy: Refusal = {
      message: 'Another large Query is running. Run them one at a time.',
      busy: true,
      estimate: { ...overBudget.estimate!, bytes: 1_610_612_736, over_budget: false },
      largest_range: null,
      top_files: [],
      narrower_query: null,
    }

    expect(refusalView(busy)).toEqual({
      message: 'Another large Query is running. Run them one at a time.', estimate: null, range: null, files: [], runAnyway: false,
    })
  })

  it('shows no estimate on an install that refuses nothing for its size', () => {
    const unbudgeted = { ...overBudget, estimate: { ...overBudget.estimate!, budget_bytes: 0 }, largest_range: null, top_files: [] }

    expect(refusalView(unbudgeted).estimate).toBeNull()
  })
})
