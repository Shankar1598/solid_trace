// The Issues page's filters, as IssuesController#index passes them back.
export interface IssueFilters {
  status: string
  query: string
  environment: string
  project_id: string
  range: string
  sort: string
}

export const TIME_RANGE_OPTIONS = { '24h': 'Last 24 hours', '14d': 'Last 14 days', '30d': 'Last 30 days', all: 'All time' }

// The Issues page's query string for filters with changes applied, leaving
// out each filter at its default.
export function issuesFilterQuery(filters: IssueFilters, changes: Partial<IssueFilters>): string {
  const params = new URLSearchParams({ ...filters, ...changes })
  if (params.get('status') === 'all') params.delete('status')
  if (params.get('environment') === 'all') params.delete('environment')
  if (params.get('project_id') === 'all') params.delete('project_id')
  if (!params.get('query')) params.delete('query')
  if (params.get('range') === '14d') params.delete('range')
  if (params.get('sort') === 'last_seen') params.delete('sort')
  return `?${params.toString()}`
}
