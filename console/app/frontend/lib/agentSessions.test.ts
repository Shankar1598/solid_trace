import { describe, expect, it } from 'vitest'
import { agentSessionsSearch, costText, tokensText, whoText, type AgentSessionRow } from './agentSessions'

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
