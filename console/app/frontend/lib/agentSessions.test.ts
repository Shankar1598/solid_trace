import { describe, expect, it } from 'vitest'
import { agentSessionsSearch, agoText, costText, modelText, nextOrder, orderOf, orderText, searchAcrossHref, titleOf, tokensText, whoText, type AgentSessionRow } from './agentSessions'

const none = { person: null, repository: null, project: null, run: null }

describe('the search of the Agent sessions page', () => {
  it('holds a span as a span, and only the filters that are set', () => {
    expect(agentSessionsSearch({ kind: 'relative', span: '7d' }, none)).toBe('?range=7d')
    expect(agentSessionsSearch({ kind: 'relative', span: '30d' }, { ...none, person: 3, run: 'none' })).toBe('?range=30d&person=3&run=none')
  })

  it('holds absolute times as UTC ISO times, and the repository and Project', () => {
    const range = { kind: 'absolute' as const, from: new Date('2026-10-02T09:00:00Z'), to: new Date('2026-10-02T10:00:00Z') }
    expect(agentSessionsSearch(range, { ...none, repository: 'github.com/acme/shop', project: 'shop' }))
      .toBe('?from=2026-10-02T09%3A00%3A00.000Z&to=2026-10-02T10%3A00%3A00.000Z&repository=github.com%2Facme%2Fshop&project=shop')
  })
})

describe('the order of the Agent sessions page', () => {
  const week = { kind: 'relative' as const, span: '7d' }

  it('holds the sort and the direction in the search, after the filters', () => {
    expect(agentSessionsSearch(week, { ...none, person: 3 }, { sort: 'tokens', direction: 'desc' })).toBe('?range=7d&person=3&sort=tokens&direction=desc')
    expect(agentSessionsSearch(week, none, { sort: 'cost', direction: 'asc' })).toBe('?range=7d&sort=cost&direction=asc')
  })

  it('leaves the default order out of the search: the latest activity first', () => {
    expect(agentSessionsSearch(week, none)).toBe('?range=7d')
    expect(agentSessionsSearch(week, none, { sort: null, direction: null })).toBe('?range=7d')
    expect(agentSessionsSearch(week, none, { sort: 'last', direction: 'desc' })).toBe('?range=7d')
    expect(agentSessionsSearch(week, none, { sort: null, direction: 'asc' })).toBe('?range=7d&sort=last&direction=asc')
  })

  it('is the last activity descending when the URL names no order', () => {
    expect(orderOf({ sort: null, direction: null })).toEqual({ sort: 'last', direction: 'desc' })
    expect(orderOf({ sort: 'start', direction: null })).toEqual({ sort: 'start', direction: 'desc' })
  })

  it('sorts a new column descending first, and flips the sorted column', () => {
    expect(nextOrder({ sort: null, direction: null }, 'tokens')).toEqual({ sort: 'tokens', direction: 'desc' })
    expect(nextOrder({ sort: 'tokens', direction: 'desc' }, 'tokens')).toEqual({ sort: 'tokens', direction: 'asc' })
    expect(nextOrder({ sort: 'tokens', direction: 'asc' }, 'tokens')).toEqual({ sort: 'tokens', direction: 'desc' })
    expect(nextOrder({ sort: 'tokens', direction: 'asc' }, 'cost')).toEqual({ sort: 'cost', direction: 'desc' })
    // The default order is the last activity descending, so a click on it flips it.
    expect(nextOrder({ sort: null, direction: null }, 'last')).toEqual({ sort: 'last', direction: 'asc' })
  })

  it('says the order in words', () => {
    expect(orderText({ sort: null, direction: null })).toBe('latest activity first')
    expect(orderText({ sort: 'last', direction: 'asc' })).toBe('earliest activity first')
    expect(orderText({ sort: 'start', direction: 'desc' })).toBe('latest start first')
    expect(orderText({ sort: 'start', direction: 'asc' })).toBe('earliest start first')
    expect(orderText({ sort: 'tokens', direction: 'desc' })).toBe('most tokens first')
    expect(orderText({ sort: 'tokens', direction: 'asc' })).toBe('fewest tokens first')
    expect(orderText({ sort: 'cost', direction: 'desc' })).toBe('highest cost first')
    expect(orderText({ sort: 'cost', direction: 'asc' })).toBe('lowest cost first')
  })
})

describe('the numbers of an Agent session', () => {
  it('gives tokens short', () => {
    expect(tokensText(950)).toBe('950')
    expect(tokensText(12_345)).toBe('12.3K')
    expect(tokensText(4_000_000)).toBe('4M')
    expect(tokensText(1_250_000_000)).toBe('1.3B')
  })

  it('gives a cost in dollars, and no cost as no cost, never $0', () => {
    expect(costText(3.5)).toBe('$3.50')
    expect(costText(1234.567)).toBe('$1,234.57')
    expect(costText(0.00174)).toBe('<$0.01')
    expect(costText(0)).toBe('$0.00')
    expect(costText(null)).toBe('No cost')
  })

  it('gives the model with its effort, or the model alone', () => {
    expect(modelText('claude-opus-5-5', 'high')).toBe('claude-opus-5-5 · high')
    expect(modelText('claude-opus-5-5', '')).toBe('claude-opus-5-5')
    expect(modelText('claude-opus-5-5', undefined)).toBe('claude-opus-5-5')
    expect(modelText('', '')).toBe('')
  })
})

describe('who an Agent session is of', () => {
  const row = { person: { id: 1, name: 'Bob' }, run: null } as Pick<AgentSessionRow, 'person' | 'run'>

  it('is the person, or the Run with its Project', () => {
    expect(whoText(row)).toBe('Bob')
    expect(whoText({ ...row, run: { id: 'run_42', project: 'shop' } })).toBe('Run run_42 (shop)')
    expect(whoText({ person: null, run: { id: 'run_42', project: null } })).toBe('Run run_42')
    expect(whoText({ person: null, run: null })).toBe('Unknown')
  })
})

describe('the title of an Agent session', () => {
  const id = '0b6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41'

  it('is its title', () => {
    expect(titleOf({ id, title: 'Cart page and checkout' })).toEqual({ text: 'Cart page and checkout', untitled: false })
  })

  it('is the short id of an Agent session with no title', () => {
    expect(titleOf({ id, title: '' })).toEqual({ text: '0b6c2f1e', untitled: true })
    expect(titleOf({ id, title: '  ' })).toEqual({ text: '0b6c2f1e', untitled: true })
  })
})

describe('the search across Agent sessions', () => {
  it('opens the Logs explorer on the Agent Project with a search over 7 days', () => {
    const href = searchAcrossHref({ logs_path: '/acme/projects/agent-sessions/logs', project: 'agent-sessions', range: '7d' }, 'the "cart" page')
    const [path, search] = href.split('?')
    expect(path).toBe('/acme/projects/agent-sessions/logs')
    const params = new URLSearchParams(search)
    expect(params.get('range')).toBe('7d')
    expect(params.get('mode')).toBe('builder')
    expect(params.get('q')).toBe(`['agent-sessions']\n| search "the \\"cart\\" page"`)
  })
})

describe('agoText', () => {
  const now = new Date('2026-11-23T12:00:00Z')

  it('gives a few seconds in the first minute, also for a time after now', () => {
    expect(agoText('2026-11-23T12:00:00Z', now)).toBe('a few seconds ago')
    expect(agoText('2026-11-23T11:59:01Z', now)).toBe('a few seconds ago')
    expect(agoText('2026-11-23T12:00:05Z', now)).toBe('a few seconds ago')
  })

  it('gives whole minutes, hours and days after that', () => {
    expect(agoText('2026-11-23T11:59:00Z', now)).toBe('1 minute ago')
    expect(agoText('2026-11-23T11:55:30Z', now)).toBe('4 minutes ago')
    expect(agoText('2026-11-23T09:10:00Z', now)).toBe('2 hours ago')
    expect(agoText('2026-11-21T10:00:00Z', now)).toBe('2 days ago')
  })
})
