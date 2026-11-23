import { describe, expect, it } from 'vitest'
import type { LogTable, TableColumn } from '@/lib/logs/table'
import { MAX_SERIES, chartOf } from './series'
import { thresholdMarks, thresholdReach } from './thresholds'

const TIME: TableColumn = { name: 'time', type: 'datetime', bin_seconds: 300 }
const REGION: TableColumn = { name: 'region', type: 'string' }
const AGE: TableColumn = { name: 'max_age', type: 'number', agg: 'max' }

describe('threshold lines', () => {
  it('are one line each, in the order given, whatever the groups, and take none of the series', () => {
    const regions = Array.from({ length: MAX_SERIES }, (_, i) => `r${i}`)
    const table: LogTable = {
      columns: [TIME, REGION, AGE],
      rows: regions.map((region, i) => ['2026-10-09T10:00:00Z', region, i]),
      truncated: false,
    }
    const model = chartOf(table)
    const marks = thresholdMarks([{ value: 1, label: 'SLO' }, { value: 0.5 }], {})

    expect(marks.map(m => [m.value, m.label])).toEqual([[1, 'SLO'], [0.5, '0.5']])
    if (model.kind !== 'time') throw new Error(`expected a time series, got ${model.kind}`)
    expect(model.series.map(s => s.name)).toEqual([...regions].reverse())
    expect(model.legend).toHaveLength(MAX_SERIES)
  })

  it('show their label, else the value with the Chart\'s unit and decimals', () => {
    const marks = thresholdMarks([{ value: 100, label: 'full' }, { value: 1 }, { value: 80 }], { unit: '%', decimals: 1 })

    expect(marks.map(m => m.label)).toEqual(['full', '1.0%', '80.0%'])
    expect(thresholdMarks([{ value: 1500 }], { unit: 'ms' })[0].label).toBe('1,500 ms')
  })

  it('take the muted colour, the warning colour or the danger colour by severity, which both themes define', () => {
    const marks = thresholdMarks([{ value: 1 }, { value: 2, severity: 'warn' }, { value: 3, severity: 'critical' }], {})

    expect(marks.map(m => m.color)).toEqual(['var(--muted-foreground)', 'var(--warning)', 'var(--destructive)'])
  })

  it('are none when not given', () => {
    expect(thresholdMarks(undefined, {})).toEqual([])
  })
})

describe('a value axis with threshold lines', () => {
  it('reaches a line above or below every value', () => {
    expect(thresholdReach([0.1, 0.4], [{ value: 1 }])).toEqual([0.1, 1])
    expect(thresholdReach([5, 9], [{ value: 2 }, { value: 7 }])).toEqual([2, 9])
  })

  it('stays as it is with none, and reaches the lines alone with no values shown', () => {
    expect(thresholdReach([3, 4], [])).toEqual([3, 4])
    expect(thresholdReach([Infinity, -Infinity], [{ value: 1 }, { value: 3 }])).toEqual([1, 3])
  })
})
