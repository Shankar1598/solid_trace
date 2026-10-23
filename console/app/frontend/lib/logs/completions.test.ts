import { describe, expect, it } from 'vitest'
import { completeQuery, type CompletionSources } from './completions'

const SOURCES: CompletionSources = {
  fields: [
    { name: 'time', type: 'time' },
    { name: 'level', type: 'string' },
    { name: 'service', type: 'string' },
    { name: 'message', type: 'string' },
    { name: 'duration_ms', type: 'number' },
    { name: 'user agent', type: 'string' },
  ],
  valuesOf: field => (field === 'level' ? ['error', 'warn'] : field === 'duration_ms' ? [120, 85] : []),
  source: "['my-api']",
}

/** Completions where | marks the cursor. */
function at(text: string, sources = SOURCES, explicit = false) {
  const pos = text.indexOf('|CURSOR|')
  const plain = text.replace('|CURSOR|', '')
  return completeQuery(plain, pos < 0 ? plain.length : pos, sources, explicit)
}
const labels = (text: string, sources = SOURCES, explicit = false) => at(text, sources, explicit)?.options.map(o => o.label) ?? null

describe('stages', () => {
  it('are offered after a |, with the word typed so far replaced', () => {
    const answer = at("['my-api'] | wh")

    expect(answer?.from).toBe("['my-api'] | ".length)
    expect(answer?.options.map(o => o.label)).toContain('where')
    expect(answer?.options.find(o => o.label === 'where')).toMatchObject({ type: 'stage', apply: 'where ', reopen: true })
    expect(answer?.options.find(o => o.label === 'summarize')?.detail).toBe('agg, … by expr, …')
  })

  it('are offered straight after a |, on the same line or the next', () => {
    expect(labels("['my-api'] |")).toContain('take')
    expect(labels("['my-api']\n| ")).toContain('take')
  })

  it('get a space before them when the | has none after it', () => {
    expect(at("['my-api'] |wh")?.options.find(o => o.label === 'where')?.apply).toBe(' where ')
  })

  it('come with the Project at the start of a Query', () => {
    const start = labels('')
    expect(start?.[0]).toBe("['my-api']")
    expect(start).toContain('where')
  })

  it('come after their own aliases', () => {
    const offered = labels("['my-api'] | ")!
    expect(offered.indexOf('sort by')).toBeLessThan(offered.indexOf('order by'))
    expect(offered.indexOf('take')).toBeLessThan(offered.indexOf('limit'))
  })
})

describe('Fields and functions', () => {
  it('are offered for a condition, Fields with their types', () => {
    const answer = at("['my-api'] | where ")

    expect(answer?.options.find(o => o.label === 'duration_ms')).toMatchObject({ type: 'field', detail: 'number' })
    expect(answer?.options.find(o => o.label === 'level')).toMatchObject({ type: 'field', detail: 'string' })
    expect(answer?.options.find(o => o.label === 'tolower')).toMatchObject({ type: 'function', detail: '(s)' })
    // Aggregates belong to summarize.
    expect(answer?.options.map(o => o.label)).not.toContain('avg')
  })

  it('quote a name that is not a plain word', () => {
    expect(at("['my-api'] | where us")?.options.find(o => o.label === 'user agent')?.apply).toBe("['user agent'] ")
  })

  it('offer aggregates first in summarize, then Fields inside them', () => {
    const aggregates = at("['my-api'] | summarize ")
    expect(aggregates?.options[0]).toMatchObject({ label: 'count', type: 'function', detail: '()' })
    expect(aggregates?.options.find(o => o.label === 'percentile')?.detail).toBe('(x, p)')
    expect(aggregates?.options.find(o => o.label === 'avg')).toMatchObject({ apply: 'avg()', cursor: 4, reopen: true })
    expect(aggregates?.options.find(o => o.label === 'rate')?.detail).toBe('()')
    expect(aggregates?.options.find(o => o.label === 'peak_rate')?.detail).toBe('()')

    expect(labels("['my-api'] | summarize avg(du")).toContain('duration_ms')
    expect(labels("['my-api'] | summarize avg(du")).not.toContain('count')
  })

  it('offer by after an aggregate, then Fields and time bins', () => {
    expect(labels("['my-api'] | summarize count() ")).toEqual(['by'])
    const groups = at("['my-api'] | summarize count() by ")!.options
    expect(groups.slice(0, 2).map(o => o.label)).toEqual(['bin_auto', 'bin'])
    expect(groups.map(o => o.label)).toContain('service')
    // Time bins come written out: bin_auto whole, bin waiting for its size.
    expect(groups[0]).toMatchObject({ apply: 'bin_auto(time)', cursor: 'bin_auto(time)'.length })
    expect(groups[1]).toMatchObject({ apply: 'bin(time, )', cursor: 'bin(time, '.length })
  })

  it('offer by after sort, and an order after a sort key', () => {
    expect(labels("['my-api'] | sort ")).toEqual(['by'])
    expect(labels("['my-api'] | sort by time ")).toEqual(['desc', 'asc', 'nulls first', 'nulls last'])
  })

  it('wait for a word before opening in the middle of an expression', () => {
    expect(at("['my-api'] | project service, ")).toBeNull()
    expect(labels("['my-api'] | project service, ", SOURCES, true)).toContain('message')
    expect(labels("['my-api'] | project service, me")).toContain('message')
  })
})

describe('operators', () => {
  it('are offered after a Field in a condition', () => {
    const answer = at("['my-api'] | where level ")

    expect(answer?.options[0]).toMatchObject({ label: '==', type: 'operator', apply: '== ', reopen: true })
    expect(answer?.options.map(o => o.label)).toEqual(expect.arrayContaining(['!=', 'contains', '!in', 'matches regex']))
  })

  it('narrow to what is typed, symbols too', () => {
    const answer = at("['my-api'] | where level !")
    expect(answer?.from).toBe("['my-api'] | where level ".length)
    expect(answer?.options.map(o => o.label)).toContain('!contains')
  })

  it('give and and or after a whole condition, once a space follows it', () => {
    expect(labels(`['my-api'] | where level == "error" `)).toEqual(['and', 'or'])
    expect(labels(`['my-api'] | where level == $level `)).toEqual(['and', 'or'])
    expect(at(`['my-api'] | where level == "error"`)).toBeNull()
    expect(at("['my-api'] | summarize count()")).toBeNull()
  })
})

describe('values', () => {
  it('are the recent values of the Field compared, as the language writes them', () => {
    const answer = at(`['my-api'] | where level == `)

    expect(answer?.options).toEqual([
      expect.objectContaining({ label: '"error"', type: 'value', detail: 'string' }),
      expect.objectContaining({ label: '"warn"', type: 'value' }),
    ])
    expect(labels(`['my-api'] | where duration_ms > `)).toEqual(['120', '85'])
  })

  it('replace a string being typed, quote and all', () => {
    const answer = at(`['my-api'] | where level == "er`)
    expect(answer?.from).toBe(`['my-api'] | where level == `.length)
    expect(answer?.options.map(o => o.label)).toEqual(['"error"', '"warn"'])
  })

  it('are offered in a list after in', () => {
    expect(labels(`['my-api'] | where level in ("error", `)).toEqual(['"error"', '"warn"'])
  })

  it('are strings only after a text operator', () => {
    expect(labels(`['my-api'] | where duration_ms contains `)).toBeNull()
  })

  it('are not offered when none are known', () => {
    expect(at(`['my-api'] | where message == `)).toBeNull()
  })
})

describe('where nothing is offered', () => {
  it('inside a comment, a search string, or after take', () => {
    expect(at("['my-api'] // wh")).toBeNull()
    expect(at(`['my-api'] | search "bo`)).toBeNull()
    expect(at("['my-api'] | take ")).toBeNull()
  })

  it('once a word is typed in full and starts no other option', () => {
    expect(at("['my-api'] | sort by time desc")).toBeNull()
    expect(at("['my-api'] | where")).toBeNull()
    // The editor narrows the list to what is typed; countif still starts with count.
    expect(labels("['my-api'] | summarize count")).toContain('countif')
    expect(labels("['my-api'] | where", SOURCES, true)).toContain('where')
  })

  it('on a Dashboard filter being typed', () => {
    expect(at("['my-api'] | where service == $se")).toBeNull()
    expect(at("['my-api'] | where $se")).toBeNull()
    expect(at("['my-api'] | search $")).toBeNull()
  })

  it('reads only the text before the cursor', () => {
    expect(at("['my-api'] | wh|CURSOR| level == 1")?.options.map(o => o.label)).toContain('where')
  })
})
