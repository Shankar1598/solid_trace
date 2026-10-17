import { describe, expect, it } from 'vitest'
import fixture from '../../../../test/fixtures/files/explorer_links.json'
import { stateFromSearch, type ExplorerState, type Mode, type TimeRange, type Viz } from './explorerState'

// The MCP server's links to the explorer. The Rails test of the same fixture
// (test/mcp/logs_tools_test.rb) checks that McpTools::Links#explorer builds
// each url; this checks that the explorer opens each in the state the agent
// ran.

interface FixtureState {
  range: { kind: string; span?: string; from?: string; to?: string }
  mode: string
  query: string
  sql: string
  viz: string
}

function expected({ range, mode, query, sql, viz }: FixtureState): ExplorerState {
  const timeRange: TimeRange =
    range.kind === 'absolute'
      ? { kind: 'absolute', from: new Date(range.from ?? ''), to: new Date(range.to ?? '') }
      : { kind: 'relative', span: range.span ?? '' }
  return { range: timeRange, mode: mode as Mode, query, sql, viz: viz as Viz }
}

describe('the MCP server’s explorer links', () => {
  it.each(fixture.links.map(link => [link.url, link.state] as const))('opens %s as the agent ran it', (url, state) => {
    expect(stateFromSearch(new URL(url).search)).toEqual(expected(state))
  })
})
