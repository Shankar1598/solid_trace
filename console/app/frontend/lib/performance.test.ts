import { describe, expect, it } from 'vitest'
import { DEFAULT_SORT, changeText, nextSort, sortTransactions, transactionHref, type TransactionRow } from './performance'

function row(label: string, p95: number, consumed: number): TransactionRow {
  return {
    name: label, label, sampled: 1, throughput_per_minute: 1, p50_us: 0, p95_us: p95, p99_us: 0, error_rate: 0,
    time_consumed_us: consumed, previous: null, change: null,
  }
}

describe('sorting Performance', () => {
  const rows = [row('b', 300, 10), row('a', 100, 50), row('c', 200, 50)]

  it('opens with the most time consuming first, names A to Z where they tie', () => {
    expect(sortTransactions(rows, DEFAULT_SORT).map(r => r.label)).toEqual(['a', 'c', 'b'])
  })

  it('sorts a number column largest first, then the other way on a second click', () => {
    const p95 = nextSort(DEFAULT_SORT, 'p95_us')
    expect(sortTransactions(rows, p95).map(r => r.label)).toEqual(['b', 'c', 'a'])
    expect(sortTransactions(rows, nextSort(p95, 'p95_us')).map(r => r.label)).toEqual(['a', 'c', 'b'])
  })

  it('sorts names A to Z first', () => {
    expect(sortTransactions(rows, nextSort(DEFAULT_SORT, 'label')).map(r => r.label)).toEqual(['a', 'b', 'c'])
  })
})

describe('a number\'s change', () => {
  it('is a share of the previous number, or the error rate\'s difference in points', () => {
    expect(changeText('p95_us', 0.9)).toBe('+90%')
    expect(changeText('throughput_per_minute', -0.034)).toBe('−3.4%')
    expect(changeText('error_rate', 1 / 7)).toBe('+14 pts')
    expect(changeText('p50_us', 0)).toBe('±0')
    expect(changeText('p50_us', null)).toBeNull()
  })
})

describe('a Transaction\'s link', () => {
  it('names it whatever it holds, with Performance\'s range and environment', () => {
    expect(transactionHref('acme', 'api', 'OrdersController#show', { kind: 'relative', span: '7d' }, 'production'))
      .toBe('/acme/projects/api/performance/transaction?name=OrdersController%23show&range=7d&environment=production')
  })
})
