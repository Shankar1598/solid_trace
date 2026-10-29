import { describe, expect, it } from 'vitest'
import type { Builder } from '@/lib/logs/builder'
import type { LogTable } from '@/lib/logs/table'
import {
  breached, changeText, groupLabel, groupsNow, newMonitorHref, rangeMinutesOf, readingText, ruleText, runText, withRangeBins,
  withoutTimeBins,
} from './monitor'

const table = (columns: LogTable['columns'], rows: unknown[][]): LogTable => ({ columns, rows, truncated: false })
const COUNT = { name: 'count_', type: 'number' as const, agg: 'count' as const }
const SERVICE = { name: 'service', type: 'string' as const }

describe('breached', () => {
  it('compares a value with the threshold by the operator', () => {
    expect(breached(5, '>', 5)).toBe(false)
    expect(breached(5, '>=', 5)).toBe(true)
    expect(breached(4, '<', 5)).toBe(true)
    expect(breached(6, '<=', 5)).toBe(false)
  })
})

describe('groupsNow', () => {
  const rule = { operator: '>' as const, threshold: 10, notify_by_group: true, alert_on_no_data: false }

  it('keys each row by its other columns, firing those past the threshold', () => {
    const { groups } = groupsNow(table([SERVICE, COUNT], [['api', 12], ['web', 3]]), rule)
    expect(groups).toEqual([
      { group: { service: 'api' }, value: 12, firing: true },
      { group: { service: 'web' }, value: 3, firing: false },
    ])
  })

  it('takes the worst row as the one group when the Monitor does not notify by group', () => {
    const { groups } = groupsNow(table([SERVICE, COUNT], [['api', 12], ['web', 30]]), { ...rule, notify_by_group: false })
    expect(groups).toEqual([{ group: {}, value: 30, firing: true }])
    const below = groupsNow(table([SERVICE, COUNT], [['api', 12], ['web', 3]]), { ...rule, operator: '<', notify_by_group: false })
    expect(below.groups).toEqual([{ group: {}, value: 3, firing: true }])
  })

  it('skips rows with no number', () => {
    const { groups } = groupsNow(table([SERVICE, COUNT], [['api', null]]), rule)
    expect(groups).toEqual([])
  })

  it('fires the one group for no rows only with alert_on_no_data', () => {
    expect(groupsNow(table([COUNT], []), rule).groups).toEqual([])
    expect(groupsNow(table([COUNT], []), { ...rule, alert_on_no_data: true }).groups).toEqual([{ group: {}, value: null, firing: true }])
  })

  it('says why a table with other than one number can not be compared', () => {
    const two = groupsNow(table([COUNT, { name: 'avg_duration', type: 'number' }], [[1, 2]]), rule)
    expect(two.error).toBe('The Query gives 2 numbers per group, not one to compare with the threshold.')
    expect(two.groups).toEqual([])
  })

  it('compares nothing without a threshold', () => {
    const { groups } = groupsNow(table([COUNT], [[12]]), { ...rule, threshold: null })
    expect(groups).toEqual([{ group: {}, value: 12, firing: false }])
  })
})

describe('withRangeBins', () => {
  const builder: Builder = {
    filters: [{ field: 'level', op: '==', value: 'error' }],
    search: '',
    summarize: { aggregates: [{ fn: 'count' }], by: [{ field: 'service' }] },
    sort: [{ column: 'count_', asc: false }],
    take: 5,
  }

  it('bins time by the range first, and drops sort and take so every bin charts', () => {
    expect(withRangeBins(builder, 15)).toEqual({
      filters: builder.filters,
      search: '',
      summarize: { aggregates: [{ fn: 'count' }], by: [{ field: 'time', bin: '15m' }, { field: 'service' }] },
    })
  })

  it('replaces a time bin the Query already has', () => {
    const binned = { ...builder, summarize: { aggregates: [{ fn: 'count' as const }], by: [{ field: 'time', bin: 'auto' }] } }
    expect(withRangeBins(binned, 5)?.summarize?.by).toEqual([{ field: 'time', bin: '5m' }])
  })

  it('has nothing to chart without a function', () => {
    expect(withRangeBins({ filters: [], search: '' }, 5)).toBeNull()
  })
})

describe('withoutTimeBins', () => {
  it('drops time bins and sorts on them, keeping the other by columns', () => {
    const builder: Builder = {
      filters: [],
      search: '',
      summarize: { aggregates: [{ fn: 'count' }], by: [{ field: 'time', bin: 'auto' }, { field: 'service' }] },
      sort: [{ column: 'time', asc: true }, { column: 'count_', asc: false }],
    }
    expect(withoutTimeBins(builder)).toEqual({
      filters: [],
      search: '',
      summarize: { aggregates: [{ fn: 'count' }], by: [{ field: 'service' }] },
      sort: [{ column: 'count_', asc: false }],
    })
  })

  it('leaves a Builder with no time bins as it is', () => {
    const builder: Builder = { filters: [], search: '', summarize: { aggregates: [{ fn: 'count' }], by: [] } }
    expect(withoutTimeBins(builder)).toBe(builder)
  })
})

describe('rangeMinutesOf', () => {
  it('reads a span or an absolute range as whole minutes, within a Monitor\'s bounds', () => {
    expect(rangeMinutesOf({ kind: 'relative', span: '15m' })).toBe(15)
    expect(rangeMinutesOf({ kind: 'relative', span: '24h' })).toBe(1440)
    expect(rangeMinutesOf({ kind: 'relative', span: '30d' })).toBe(7 * 24 * 60)
    expect(rangeMinutesOf({ kind: 'absolute', from: new Date('2026-10-03T09:00:00Z'), to: new Date('2026-10-03T09:00:20Z') })).toBe(1)
  })
})

describe('newMonitorHref', () => {
  it('opens the New page with the Project, Query and range', () => {
    expect(newMonitorHref('acme', 7, "['shop'] | summarize count()", { kind: 'relative', span: '1h' }))
      .toBe("/acme/monitors/new?project_id=7&query=%5B%27shop%27%5D+%7C+summarize+count%28%29&range_minutes=60")
  })
})

describe('groupLabel', () => {
  it('names a group by its columns, or the Monitor\'s one group', () => {
    expect(groupLabel({ service: 'api', region: 'eu' })).toBe('service: api, region: eu')
    expect(groupLabel({})).toBe('All Logs')
  })
})

describe('runText', () => {
  const run = { status: 'ok' as const, error: null, firing: 0, matched: null, logs_skipped: false }

  it('says what a run found', () => {
    expect(runText(run, 'threshold')).toBe('Nothing firing')
    expect(runText({ ...run, firing: 2 }, 'threshold')).toBe('2 groups firing')
    expect(runText({ ...run, firing: null, matched: 1 }, 'match')).toBe('1 Log matched')
    expect(runText({ ...run, status: 'error', error: 'EventStore is unavailable' }, 'threshold')).toBe('EventStore is unavailable')
  })

  it('says when a Match monitor skipped Logs to start again', () => {
    expect(runText({ ...run, firing: null, matched: 0, logs_skipped: true }, 'match'))
      .toBe('0 Logs matched. Started again from the newest Logs, skipping those ingested since the last run.')
  })
})

describe('changeText', () => {
  it('gives a change from the Baseline as a signed percent, to at most one decimal', () => {
    expect(changeText(120, 60)).toBe('+100%')
    expect(changeText(7, 10)).toBe('-30%')
    expect(changeText(70, 60)).toBe('+16.7%')
    expect(changeText(-50, -100)).toBe('+50%')
    expect(changeText(0, 0)).toBe('0%')
  })

  it('reads a change from a Baseline of 0 as up or down from 0', () => {
    expect(changeText(5, 0)).toBe('up from 0')
    expect(changeText(-3, 0)).toBe('down from 0')
  })
})

describe('readingText', () => {
  it('gives a number with its Baseline and change, or the number alone without a Baseline', () => {
    expect(readingText(120, 60)).toBe('120 (baseline 60, +100%)')
    expect(readingText(5, 0)).toBe('5 (baseline 0, up from 0)')
    expect(readingText(42, null)).toBe('42')
    expect(readingText(null, null)).toBe('No data')
  })
})

describe('ruleText', () => {
  const monitor = {
    kind: 'threshold' as const, compare: 'value' as const, baseline: null, interval_minutes: 1, range_minutes: 15,
    operator: '>' as const, threshold: 10, notify_by_group: true, alert_on_no_data: false,
  }

  it('says what a Threshold monitor compares', () => {
    expect(ruleText(monitor)).toBe("Runs every minute over the last 15 minutes, and fires when each group's number is > 10.")
  })

  it('describes the change rule for a Monitor comparing the change', () => {
    const change = { ...monitor, compare: 'change' as const, baseline: 'previous' as const, threshold: 50 }
    expect(ruleText({ ...change, operator: 'rises' })).toBe(
      "Runs every minute over the last 15 minutes, and fires when each group's number rises by 50% or more from the 15 minutes before.")
    expect(ruleText({ ...change, operator: 'falls', notify_by_group: false, alert_on_no_data: true })).toBe(
      "Runs every minute over the last 15 minutes, and fires when the worst group's number falls by 50% or more from the 15 minutes before. No rows at all fire too.")
    expect(ruleText({ ...change, operator: 'changes' })).toContain('changes by 50% or more either way from the 15 minutes before')
  })
})
