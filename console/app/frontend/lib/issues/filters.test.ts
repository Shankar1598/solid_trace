import { describe, expect, it } from 'vitest'
import { issuesFilterQuery, type IssueFilters } from './filters'

const defaults: IssueFilters = { status: 'all', query: '', environment: 'all', project_id: 'all', range: '14d', sort: 'last_seen' }

describe('issuesFilterQuery', () => {
  it('leaves out every filter at its default', () => {
    expect(issuesFilterQuery(defaults, {})).toBe('?')
  })

  it('carries a chosen range', () => {
    expect(issuesFilterQuery(defaults, { range: '24h' })).toBe('?range=24h')
    expect(issuesFilterQuery(defaults, { range: 'all' })).toBe('?range=all')
  })

  it('keeps the other filters when the range changes', () => {
    expect(issuesFilterQuery({ ...defaults, project_id: '3', query: 'boom' }, { range: '30d' }))
      .toBe('?query=boom&project_id=3&range=30d')
  })
})
