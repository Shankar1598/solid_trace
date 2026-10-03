import { describe, expect, it } from 'vitest'
import {
  EMPTY_BUILDER,
  builderOf,
  canFilter,
  flipFilter,
  hide,
  queryOf,
  removeFilter,
  search,
  showOnly,
  type Builder,
} from './builder'

describe('the Query text of a Builder', () => {
  it('is empty with no chips and no search', () => {
    expect(queryOf(EMPTY_BUILDER)).toBe('')
  })

  it('writes each chip as its own where stage, in order, then the search', () => {
    const builder: Builder = {
      filters: [
        { field: 'service', op: '==', value: 'api' },
        { field: 'level', op: '!=', value: 'debug' },
      ],
      search: 'timeout',
    }

    expect(queryOf(builder)).toBe('where service == "api"\n| where level != "debug"\n| search "timeout"')
  })

  it('writes a search alone as the one search stage', () => {
    expect(queryOf({ filters: [], search: 'boom' })).toBe('search "boom"')
  })

  it('leaves out a search of only spaces', () => {
    expect(queryOf({ filters: [], search: '   ' })).toBe('')
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

    expect(queryOf(builder)).toBe(
      'where status == 500\n| where status != "500"\n| where ratio == -0.25\n| where cached == false',
    )
  })

  it('escapes quotes, backslashes and line breaks in strings', () => {
    const builder: Builder = {
      filters: [{ field: 'path', op: '==', value: 'C:\\tmp\\"a"' }],
      search: 'line one\nline\ttwo\r',
    }

    expect(queryOf(builder)).toBe('where path == "C:\\\\tmp\\\\\\"a\\""\n| search "line one\\nline\\ttwo\\r"')
  })

  it('writes dotted Attribute names bare', () => {
    expect(queryOf({ filters: [{ field: 'http.request.method', op: '==', value: 'GET' }], search: '' }))
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

    expect(queryOf(builder).split('\n| ')).toEqual([
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
    expect(queryOf(builder)).toBe('where http.status != 500\n| where service == "api"\n| search "boom"')
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
    expect(queryOf(flipFilter(builder, 1))).toBe('where service == "api"\n| where level == "debug"\n| search "boom"')
  })

  it('removes a chip, keeping the rest and the search', () => {
    expect(removeFilter(builder, 0)).toEqual({ filters: [{ field: 'level', op: '!=', value: 'debug' }], search: 'boom' })
  })

  it('sets the search', () => {
    expect(search(builder, 'timeout').search).toBe('timeout')
    expect(queryOf(search(builder, ''))).toBe('where service == "api"\n| where level != "debug"')
  })
})

describe('filtering from a tag', () => {
  it('Show only this adds field == value with the value\'s own type', () => {
    expect(showOnly(EMPTY_BUILDER, 'http.status', 404).filters).toEqual([{ field: 'http.status', op: '==', value: 404 }])
    expect(showOnly(EMPTY_BUILDER, 'http.status', '404').filters).toEqual([{ field: 'http.status', op: '==', value: '404' }])
    expect(queryOf(showOnly(EMPTY_BUILDER, 'cached', true))).toBe('where cached == true')
  })

  it('Hide this adds field != value, which keeps Logs that don\'t have the Field', () => {
    // != keeps Logs missing the Field (the Query language's rule), so Hide
    // is written as != and never as a == on another value.
    const hidden = hide(EMPTY_BUILDER, 'level', 'debug')

    expect(hidden.filters).toEqual([{ field: 'level', op: '!=', value: 'debug' }])
    expect(queryOf(hidden)).toBe('where level != "debug"')
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
