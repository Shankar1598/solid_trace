import { describe, expect, it } from 'vitest'
import { monitorStatusItems, monitorStatusParam, NO_MONITOR_STATUS, notRunningSince } from './monitorStatus'

describe('monitorStatusItems', () => {
  it('starts with None, then names each Integration with its provider, and says when one is off', () => {
    const items = monitorStatusItems([
      { id: 3, name: 'Ops', provider: 'slack', enabled: true },
      { id: 4, name: 'Pager', provider: 'pagerduty', enabled: false },
    ])
    expect(items).toEqual([
      { value: NO_MONITOR_STATUS, label: 'None' },
      { value: '3', label: 'Ops (Slack)' },
      { value: '4', label: 'Pager (PagerDuty), off' },
    ])
  })
})

describe('monitorStatusParam', () => {
  it('sends an empty id for None, and the id otherwise', () => {
    expect(monitorStatusParam(NO_MONITOR_STATUS)).toBe('')
    expect(monitorStatusParam(null)).toBe('')
    expect(monitorStatusParam('3')).toBe('3')
  })
})

describe('notRunningSince', () => {
  it('shows since when a Monitor that is on does not run, and nothing for one that is off', () => {
    const since = '2026-10-17T10:15:00Z'
    expect(notRunningSince({ enabled: true, not_running_since: since })).toBe(since)
    expect(notRunningSince({ enabled: false, not_running_since: since })).toBeNull()
    expect(notRunningSince({ enabled: true, not_running_since: null })).toBeNull()
  })
})
