import { describe, expect, it } from 'vitest'
import {
  DEFAULT_COLUMNS,
  columnValue,
  commonValues,
  fieldInfos,
  columnsFromSearch,
  moveColumn,
  pickableFields,
  toggleColumn,
  withColumns,
} from './columns'
import type { Log } from './stream'

const log = (attributes: Log['attributes'], extra: Partial<Log> = {}): Log =>
  ({ id: '1', time: '2026-10-03T10:00:00Z', message: 'hi', attributes, ...extra })

describe('columns in the URL', () => {
  it('opens with time, service, level and message', () => {
    expect(columnsFromSearch('?range=1h&mode=builder')).toEqual(['time', 'service', 'level', 'message'])
  })

  it('round-trips columns in order, beside the explorer state', () => {
    const search = withColumns('?range=1h&mode=query&q=take+5', ['level', 'message', 'http.status'])
    expect(search).toBe('?range=1h&mode=query&q=take+5&col=level&col=message&col=http.status')
    expect(columnsFromSearch(search)).toEqual(['level', 'message', 'http.status'])
  })

  it('leaves the default columns out of the URL', () => {
    expect(withColumns('?range=1h&col=level', DEFAULT_COLUMNS)).toBe('?range=1h')
  })

  it('drops blank and repeated columns from a hand-edited URL', () => {
    expect(columnsFromSearch('?col=level&col=&col=level&col=%20user.id')).toEqual(['level', 'user.id'])
    expect(columnsFromSearch('?col=')).toEqual(DEFAULT_COLUMNS)
  })
})

describe('a column value', () => {
  it('reads built-in fields from the Log and the rest from its Attributes', () => {
    const l = log({ 'http.status': 502, level: 'shadowed' }, { level: 'error', service: 'api' })
    expect(columnValue(l, 'level')).toBe('error')
    expect(columnValue(l, 'service')).toBe('api')
    expect(columnValue(l, 'http.status')).toBe(502)
    expect(columnValue(l, 'missing')).toBeUndefined()
  })
})

describe('changing columns', () => {
  it('adds a column at the right end and removes it again', () => {
    const added = toggleColumn(DEFAULT_COLUMNS, 'user.id')
    expect(added).toEqual(['time', 'service', 'level', 'message', 'user.id'])
    expect(toggleColumn(added, 'service')).toEqual(['time', 'level', 'message', 'user.id'])
  })

  it('keeps the last column', () => {
    expect(toggleColumn(['message'], 'message')).toEqual(['message'])
  })

  it('moves a column before another, or to the end', () => {
    expect(moveColumn(['a', 'b', 'c', 'd'], 'd', 'b')).toEqual(['a', 'd', 'b', 'c'])
    expect(moveColumn(['a', 'b', 'c', 'd'], 'a', 'c')).toEqual(['b', 'a', 'c', 'd'])
    expect(moveColumn(['a', 'b', 'c'], 'a', null)).toEqual(['b', 'c', 'a'])
    expect(moveColumn(['a', 'b'], 'a', 'a')).toEqual(['a', 'b'])
  })
})

describe('the picker', () => {
  it('offers the built-in fields, then the Attributes the Logs have, by name', () => {
    const fields = pickableFields([log({ 'user.id': 1, 'http.status': 200 }), log({ 'http.status': 500, 'cache.hit': true })])
    expect(fields).toEqual(['time', 'level', 'service', 'environment', 'message', 'trace_id', 'span_id', 'cache.hit', 'http.status', 'user.id'])
  })
})

describe('what the Builder suggests', () => {
  const logs = [
    log({ 'http.status': 200, tags: ['a'], 'cache.hit': true, mixed: 1 }, { service: 'api' }),
    log({ 'http.status': 500, mixed: 'x' }, { service: 'api' }),
    log({ 'http.status': 200 }, { service: 'worker' }),
  ]

  it('lists the built-in fields, then the Attributes with their types', () => {
    expect(fieldInfos(logs).slice(7)).toEqual([
      { name: 'cache.hit', type: 'bool' },
      { name: 'http.status', type: 'number' },
      { name: 'mixed', type: 'mixed' },
      { name: 'tags', type: 'array' },
    ])
    expect(fieldInfos([])[0]).toEqual({ name: 'time', type: 'time' })
  })

  it('suggests a Field\'s most common values first, keeping their types', () => {
    expect(commonValues(logs, 'http.status')).toEqual([200, 500])
    expect(commonValues(logs, 'service')).toEqual(['api', 'worker'])
    expect(commonValues(logs, 'tags')).toEqual([])
  })
})
