import { describe, expect, it } from 'vitest'
import {
  EMPTY_BUILDER,
  addAggregate,
  addFilter,
  builderOf,
  canFilter,
  chartField,
  flipFilter,
  groupBy,
  hide,
  queryOf,
  removeAggregate,
  removeFilter,
  parseValue,
  editableText,
  search,
  setFilter,
  setSort,
  setSummarize,
  setTake,
  showOnly,
  summarizeColumns,
  type Builder,
  type Chip,
  type Summarize,
} from './builder'

/** A Builder's Query text after its source, for tests of its stages. */
const stagesOf = (builder: Builder) => queryOf(builder, 'my-api').replace(/^\['my-api'\](\n\| )?/, '')

describe('the Query text of a Builder', () => {
  it('is the Project\'s slug alone, in brackets, with no clauses', () => {
    expect(queryOf(EMPTY_BUILDER, 'my-api')).toBe("['my-api']")
  })

  it('starts with the Project\'s slug in brackets as its source', () => {
    expect(queryOf({ filters: [{ field: 'service', op: '==', value: 'api' }], search: '' }, 'application-logs'))
      .toBe('[\'application-logs\']\n| where service == "api"')
  })

  it('escapes a quote in the slug', () => {
    expect(queryOf(EMPTY_BUILDER, "it's")).toBe("['it\\'s']")
  })

  it('writes each chip as its own where stage, in order, then the search', () => {
    const builder: Builder = {
      filters: [
        { field: 'service', op: '==', value: 'api' },
        { field: 'level', op: '!=', value: 'debug' },
      ],
      search: 'timeout',
    }

    expect(stagesOf(builder)).toBe('where service == "api"\n| where level != "debug"\n| search "timeout"')
  })

  it('writes a search alone as the one search stage', () => {
    expect(stagesOf({ filters: [], search: 'boom' })).toBe('search "boom"')
  })

  it('leaves out a search of only spaces', () => {
    expect(stagesOf({ filters: [], search: '   ' })).toBe('')
  })

  it('writes numbers and bools bare, so a filter keeps its value\'s type', () => {
    const builder: Builder = {
      filters: [
        { field: 'status', op: '==', value: 500 },
        { field: 'status', op: '!=', value: '500' },
        { field: 'ratio', op: '==', value: -0.25 },
        { field: 'cached', op: '==', value: false },
      ],
      search: '',
    }

    expect(stagesOf(builder)).toBe(
      'where status == 500\n| where status != "500"\n| where ratio == -0.25\n| where cached == false',
    )
  })

  it('escapes quotes, backslashes and line breaks in strings', () => {
    const builder: Builder = {
      filters: [{ field: 'path', op: '==', value: 'C:\\tmp\\"a"' }],
      search: 'line one\nline\ttwo\r',
    }

    expect(stagesOf(builder)).toBe('where path == "C:\\\\tmp\\\\\\"a\\""\n| search "line one\\nline\\ttwo\\r"')
  })

  it('writes dotted Attribute names bare', () => {
    expect(stagesOf({ filters: [{ field: 'http.request.method', op: '==', value: 'GET' }], search: '' }))
      .toBe('where http.request.method == "GET"')
  })

  it('quotes names that aren\'t plain, and names the language reserves', () => {
    const builder: Builder = {
      filters: [
        { field: 'user-agent', op: '==', value: 'curl' },
        { field: "it's", op: '==', value: 1 },
        { field: '2fa', op: '==', value: true },
        { field: 'and', op: '==', value: 'x' },
        { field: 'flags.true', op: '==', value: 'x' },
        { field: 'a..b', op: '==', value: 'x' },
      ],
      search: '',
    }

    expect(stagesOf(builder).split('\n| ')).toEqual([
      "where ['user-agent'] == \"curl\"",
      "where ['it\\'s'] == 1",
      "where ['2fa'] == true",
      "where ['and'] == \"x\"",
      "where ['flags.true'] == \"x\"",
      "where ['a..b'] == \"x\"",
    ])
  })
})

describe('a Builder read back from validate', () => {
  it('takes its chips and search from validate\'s chips', () => {
    const builder = builderOf({
      filters: [
        { field: 'http.status', op: '!=', value: 500 },
        { field: 'service', op: '==', value: 'api' },
      ],
      search: 'boom',
    })

    expect(builder).toEqual({
      filters: [
        { field: 'http.status', op: '!=', value: 500 },
        { field: 'service', op: '==', value: 'api' },
      ],
      search: 'boom',
    })
    expect(stagesOf(builder)).toBe('where http.status != 500\n| where service == "api"\n| search "boom"')
  })

  it('has an empty search box when validate gives no search', () => {
    expect(builderOf({ filters: [], search: null })).toEqual(EMPTY_BUILDER)
  })
})

describe('editing chips', () => {
  const builder: Builder = {
    filters: [
      { field: 'service', op: '==', value: 'api' },
      { field: 'level', op: '!=', value: 'debug' },
    ],
    search: 'boom',
  }

  it('flips a chip\'s operator both ways', () => {
    const flipped = flipFilter(builder, 0)

    expect(flipped.filters[0]).toEqual({ field: 'service', op: '!=', value: 'api' })
    expect(flipFilter(flipped, 0)).toEqual(builder)
    expect(stagesOf(flipFilter(builder, 1))).toBe('where service == "api"\n| where level == "debug"\n| search "boom"')
  })

  it('removes a chip, keeping the rest and the search', () => {
    expect(removeFilter(builder, 0)).toEqual({ filters: [{ field: 'level', op: '!=', value: 'debug' }], search: 'boom' })
  })

  it('sets the search', () => {
    expect(search(builder, 'timeout').search).toBe('timeout')
    expect(stagesOf(search(builder, ''))).toBe('where service == "api"\n| where level != "debug"')
  })
})

describe('filtering from a tag', () => {
  it('Show only this adds field == value with the value\'s own type', () => {
    expect(showOnly(EMPTY_BUILDER, 'http.status', 404).filters).toEqual([{ field: 'http.status', op: '==', value: 404 }])
    expect(showOnly(EMPTY_BUILDER, 'http.status', '404').filters).toEqual([{ field: 'http.status', op: '==', value: '404' }])
    expect(stagesOf(showOnly(EMPTY_BUILDER, 'cached', true))).toBe('where cached == true')
  })

  it('Hide this adds field != value, which keeps Logs that don\'t have the Field', () => {
    // != keeps Logs missing the Field (the Query language's rule), so Hide
    // is written as != and never as a == on another value.
    const hidden = hide(EMPTY_BUILDER, 'level', 'debug')

    expect(hidden.filters).toEqual([{ field: 'level', op: '!=', value: 'debug' }])
    expect(stagesOf(hidden)).toBe('where level != "debug"')
  })

  it('adds after the chips already there', () => {
    const builder = showOnly(showOnly(EMPTY_BUILDER, 'service', 'api'), 'host', 'web-1')

    expect(builder.filters.map(f => f.field)).toEqual(['service', 'host'])
  })

  it('replaces a chip on the same value instead of adding a contradiction', () => {
    const builder = hide(showOnly(EMPTY_BUILDER, 'service', 'api'), 'service', 'api')

    expect(builder.filters).toEqual([{ field: 'service', op: '!=', value: 'api' }])
  })

  it('Show only this replaces the Field\'s other Show only chips, keeping its Hide chips', () => {
    let builder = hide(EMPTY_BUILDER, 'service', 'cron')
    builder = showOnly(builder, 'service', 'api')
    builder = showOnly(builder, 'service', 'web')

    expect(builder.filters).toEqual([
      { field: 'service', op: '!=', value: 'cron' },
      { field: 'service', op: '==', value: 'web' },
    ])
  })

  it('treats values of different types as different values', () => {
    const builder = hide(hide(EMPTY_BUILDER, 'status', 500), 'status', '500')

    expect(builder.filters).toEqual([
      { field: 'status', op: '!=', value: 500 },
      { field: 'status', op: '!=', value: '500' },
    ])
  })

  it('keeps the search', () => {
    expect(showOnly({ filters: [], search: 'boom' }, 'level', 'error').search).toBe('boom')
  })

  it('filters only on strings, numbers and bools', () => {
    expect(canFilter('api')).toBe(true)
    expect(canFilter(0)).toBe(true)
    expect(canFilter(false)).toBe(true)
    expect(canFilter(null)).toBe(false)
    expect(canFilter(undefined)).toBe(false)
    expect(canFilter([1, 2])).toBe(false)
    expect(canFilter({ a: 1 })).toBe(false)
  })
})

describe('the Where operators', () => {
  const where = (chip: Chip) => stagesOf({ filters: [chip], search: '' })

  it('writes comparisons, text tests, lists and presence', () => {
    expect(where({ field: 'duration_ms', op: '>=', value: 500 })).toBe('where duration_ms >= 500')
    expect(where({ field: 'path', op: 'startswith', value: '/api' })).toBe('where path startswith "/api"')
    expect(where({ field: 'tags', op: '!contains', value: 'beta' })).toBe('where tags !contains "beta"')
    expect(where({ field: 'status', op: 'in', value: [500, '502'] })).toBe('where status in (500, "502")')
    expect(where({ field: 'user.id', op: 'exists', value: null })).toBe('where isnotnull(user.id)')
    expect(where({ field: 'user-id', op: 'missing', value: null })).toBe("where isnull(['user-id'])")
  })

  it('flips each operator to its opposite', () => {
    const flip = (op: Chip['op']) => flipFilter({ filters: [{ field: 'a', op, value: 1 }], search: '' }, 0).filters[0].op
    expect([flip('>'), flip('>='), flip('contains'), flip('in'), flip('exists')]).toEqual(['<=', '<', '!contains', '!in', 'missing'])
  })

  it('adds and replaces filters', () => {
    const added = setFilter(EMPTY_BUILDER, 0, { field: 'a', op: '==', value: 1 })
    expect(setFilter(added, 0, { field: 'b', op: '<', value: 2 }).filters).toEqual([{ field: 'b', op: '<', value: 2 }])
  })
})

describe('Summarize, sort and limit', () => {
  const summarize: Summarize = {
    aggregates: [{ fn: 'count' }, { fn: 'avg', field: 'duration_ms' }, { fn: 'percentile', field: 'duration_ms', percent: 99 }],
    by: [{ field: 'service' }, { field: 'time', bin: '5m' }],
  }

  it('writes them after the filters and search, in order', () => {
    const builder: Builder = {
      filters: [{ field: 'level', op: '==', value: 'error' }], search: 'boom',
      summarize, sort: [{ column: 'count_', asc: false }], take: 20,
    }
    expect(stagesOf(builder).split('\n| ')).toEqual([
      'where level == "error"',
      'search "boom"',
      'summarize count(), avg(duration_ms), percentile(duration_ms, 99) by service, bin(time, 5m)',
      'sort by count_ desc',
      'take 20',
    ])
  })

  it('writes a Summarize of only by columns, and automatic time bins', () => {
    expect(stagesOf({ filters: [], search: '', summarize: { aggregates: [], by: [{ field: 'time', bin: 'auto' }] } }))
      .toBe('summarize by bin_auto(time)')
  })

  it('names the columns a Summarize makes, as the language does', () => {
    expect(summarizeColumns(summarize)).toEqual(['service', 'time', 'count_', 'avg_duration_ms', 'percentile_duration_ms_99'])
  })

  it('drops a sort on columns a changed Summarize no longer makes', () => {
    const builder = setSort(setSummarize(EMPTY_BUILDER, summarize), [{ column: 'avg_duration_ms', asc: true }, { column: 'service', asc: true }])
    const changed = setSummarize(builder, { aggregates: [{ fn: 'count' }], by: [{ field: 'service' }] })
    expect(changed.sort).toEqual([{ column: 'service', asc: true }])
    expect(setSummarize(builder, { aggregates: [], by: [] })).toEqual(EMPTY_BUILDER)
  })

  it('keeps a sort on Log Fields when a Summarize is removed it never had', () => {
    const sorted = setSort(EMPTY_BUILDER, [{ column: 'duration_ms', asc: false }])
    expect(setSummarize(sorted, { aggregates: [], by: [] }).sort).toEqual([{ column: 'duration_ms', asc: false }])
  })

  it('starts a new Summarize by time in automatic bins, so its first function draws a chart', () => {
    const counted = addAggregate(EMPTY_BUILDER, { fn: 'count' })

    expect(counted.summarize).toEqual({ aggregates: [{ fn: 'count' }], by: [{ field: 'time', bin: 'auto' }] })
    expect(stagesOf(counted)).toBe('summarize count() by bin_auto(time)')
  })

  it('adds a function to a Summarize as it is, by columns and all', () => {
    const byService = setSummarize(EMPTY_BUILDER, { aggregates: [], by: [{ field: 'service' }] })
    const avg = { fn: 'avg', field: 'duration_ms' } as const

    expect(addAggregate(byService, { fn: 'count' }).summarize).toEqual({ aggregates: [{ fn: 'count' }], by: [{ field: 'service' }] })
    expect(addAggregate(addAggregate(EMPTY_BUILDER, { fn: 'count' }), avg).summarize)
      .toEqual({ aggregates: [{ fn: 'count' }, avg], by: [{ field: 'time', bin: 'auto' }] })
  })

  it('removes a new Summarize with its last function, by time bins and all', () => {
    const counted = addAggregate(EMPTY_BUILDER, { fn: 'count' })

    expect(removeAggregate(counted, 0)).toEqual(EMPTY_BUILDER)
    expect(stagesOf(removeAggregate(addAggregate(counted, { fn: 'avg', field: 'duration_ms' }), 0)))
      .toBe('summarize avg(duration_ms) by bin_auto(time)')
  })

  it('keeps by columns of the user\'s own when the last function goes, but not the time bins a click added', () => {
    const byService = groupBy(EMPTY_BUILDER, 'service')
    const counted = addAggregate(EMPTY_BUILDER, { fn: 'count' })
    const countedByService = setSummarize(counted, { ...counted.summarize!, by: [...counted.summarize!.by, { field: 'service' }] })
    const byHour = setSummarize(EMPTY_BUILDER, { aggregates: [{ fn: 'count' }], by: [{ field: 'time', bin: '1h' }] })

    expect(stagesOf(removeAggregate(byService, 0))).toBe('summarize by service')
    expect(stagesOf(removeAggregate(countedByService, 0))).toBe('summarize by service')
    expect(stagesOf(removeAggregate(byHour, 0))).toBe('summarize by bin(time, 1h)')
    expect(removeAggregate(EMPTY_BUILDER, 0)).toEqual(EMPTY_BUILDER)
  })

  it('sets and clears the limit', () => {
    expect(stagesOf(setTake(EMPTY_BUILDER, 100))).toBe('take 100')
    expect(setTake(setTake(EMPTY_BUILDER, 100), null)).toEqual(EMPTY_BUILDER)
  })

  it('reads them back from validate, leaving out what is absent', () => {
    expect(builderOf({ filters: [], search: null, summarize: null, sort: [], take: null })).toEqual(EMPTY_BUILDER)
    expect(builderOf({ filters: [], search: null, summarize, sort: [{ column: 'count_', asc: false }], take: 5 }))
      .toEqual({ filters: [], search: '', summarize, sort: [{ column: 'count_', asc: false }], take: 5 })
  })
})

describe('values typed in an editor', () => {
  it('reads numbers, bools and quoted strings', () => {
    expect([parseValue('500'), parseValue('-1.5'), parseValue('true'), parseValue('"500"'), parseValue('api'), parseValue('')])
      .toEqual([500, -1.5, true, '500', 'api', ''])
  })

  it('shows a value so that it reads back the same', () => {
    for (const value of [500, '500', true, 'true', 'api']) expect(parseValue(editableText(value))).toEqual(value)
  })
})

describe('adding to the Query from the inspector', () => {
  const builder: Builder = { filters: [{ field: 'level', op: '!=', value: 'debug' }], search: '' }

  it('adds == and != as Show only this and Hide this do', () => {
    expect(addFilter(builder, { field: 'level', op: '==', value: 'error' })).toEqual(showOnly(builder, 'level', 'error'))
    expect(addFilter(builder, { field: 'service', op: '!=', value: 'api' })).toEqual(hide(builder, 'service', 'api'))
  })

  it('adds any other filter once', () => {
    const exists = addFilter(builder, { field: 'user.id', op: 'exists', value: null })
    expect(exists.filters).toEqual([...builder.filters, { field: 'user.id', op: 'exists', value: null }])
    expect(addFilter(exists, { field: 'user.id', op: 'exists', value: null })).toBe(exists)
  })

  it('counts by a Field over time, or adds it to the Summarize\'s by columns', () => {
    const counted = groupBy(builder, 'service')
    expect(counted.summarize).toEqual({ aggregates: [{ fn: 'count' }], by: [{ field: 'time', bin: 'auto' }, { field: 'service' }] })
    const avg: Builder = { ...builder, summarize: { aggregates: [{ fn: 'avg', field: 'duration_ms' }], by: [{ field: 'time', bin: 'auto' }] } }
    expect(groupBy(avg, 'service').summarize).toEqual({ aggregates: [{ fn: 'avg', field: 'duration_ms' }], by: [{ field: 'time', bin: 'auto' }, { field: 'service' }] })
    expect(groupBy(counted, 'service')).toEqual(counted)
  })
})

describe('Chart this from a value', () => {
  const builder: Builder = { filters: [{ field: 'level', op: '!=', value: 'debug' }], search: '' }

  it('charts a number\'s average over time', () => {
    expect(stagesOf(chartField(builder, 'duration_ms', true)))
      .toBe('where level != "debug"\n| summarize avg(duration_ms) by bin_auto(time)')
  })

  it('counts Logs by any other value\'s Field over time', () => {
    expect(stagesOf(chartField(builder, 'service', false)))
      .toBe('where level != "debug"\n| summarize count() by service, bin_auto(time)')
  })

  it('takes the place of the Summarize there was, and of a sort on its columns', () => {
    const sorted = setSort(groupBy(builder, 'service'), [{ column: 'count_', asc: false }])
    const charted = chartField(sorted, 'duration_ms', true)
    expect(charted.summarize).toEqual({ aggregates: [{ fn: 'avg', field: 'duration_ms' }], by: [{ field: 'time', bin: 'auto' }] })
    expect(charted.sort).toBeUndefined()
  })

  it('leaves the chart\'s time bins out once its function goes', () => {
    expect(removeAggregate(chartField(builder, 'service', false), 0).summarize).toEqual({ aggregates: [], by: [{ field: 'service' }] })
  })
})
