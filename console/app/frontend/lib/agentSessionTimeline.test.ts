import { describe, expect, it } from 'vitest'
import { fromText, timelineItems, timelinePath, type TimelineStep, type TimelineSubagent } from './agentSessionTimeline'

let offset = 0
function step(role: string, text: string, more: Partial<TimelineStep> = {}): TimelineStep {
  offset += 100
  return {
    id: `s${offset}`, time: '2026-10-02T11:00:00Z', path: 'session.jsonl', version: 1, offset, line: 'user', role, text,
    tokens: null, cost_usd: null, cut: false, from: { kind: role === 'assistant' ? 'agent' : role }, ...more,
  }
}

const subagent = (more: Partial<TimelineSubagent> = {}): TimelineSubagent => ({
  path: 'subagents/agent-a1.jsonl', agent_id: 'a1', tool_use_id: 'toolu_9', first: '2026-10-02T11:00:02Z', last: '2026-10-02T11:00:03Z', ...more,
})

describe('timelineItems', () => {
  it('puts each tool output inside its tool call, and keeps the other steps in order', () => {
    const person = step('person', 'Add a cart page')
    const call = step('assistant', '{"file_path":"cart.go"}', { line: 'assistant/tool_use', tool_name: 'Read', tool_use_id: 'toolu_1' })
    const result = step('tool', 'package cart', { line: 'user/tool_result', tool_name: 'Read', tool_use_id: 'toolu_1' })
    const reply = step('assistant', 'Done.', { line: 'assistant/text' })

    expect(timelineItems([person, call, result, reply], [], true)).toEqual([
      { kind: 'step', step: person },
      { kind: 'tool', call, result, subagent: null },
      { kind: 'step', step: reply },
    ])
  })

  it('pairs the outputs of parallel tool calls with their own calls', () => {
    const a = step('assistant', '{}', { line: 'assistant/tool_use', tool_name: 'Read', tool_use_id: 'A' })
    const b = step('assistant', '{}', { line: 'assistant/tool_use', tool_name: 'Grep', tool_use_id: 'B' })
    const rb = step('tool', 'b out', { line: 'user/tool_result', tool_use_id: 'B' })
    const ra = step('tool', 'a out', { line: 'user/tool_result', tool_use_id: 'A' })

    expect(timelineItems([a, b, rb, ra], [], true)).toEqual([
      { kind: 'tool', call: a, result: ra, subagent: null },
      { kind: 'tool', call: b, result: rb, subagent: null },
    ])
  })

  it('shows an output whose call is on an earlier page by itself', () => {
    const result = step('tool', 'late', { line: 'user/tool_result', tool_use_id: 'old' })
    expect(timelineItems([result], [], true)).toEqual([{ kind: 'tool', call: null, result, subagent: null }])
  })

  it('puts a subagent inside the tool call that started it', () => {
    const call = step('assistant', '{}', { line: 'assistant/tool_use', tool_name: 'Agent', tool_use_id: 'toolu_9' })
    const result = step('tool', 'cart.go', { line: 'user/tool_result', tool_use_id: 'toolu_9', subagent_id: 'a1' })
    const s = subagent()

    expect(timelineItems([call, result], [s], true)).toEqual([{ kind: 'tool', call, result, subagent: s }])
  })

  it('puts a subagent that no call names at its first time, once the timeline reaches it', () => {
    const before = step('person', 'One', { time: '2026-10-02T11:00:01Z' })
    const after = step('person', 'Two', { time: '2026-10-02T11:00:05Z' })
    const s = subagent({ tool_use_id: '' })

    expect(timelineItems([before, after], [s], false)).toEqual([
      { kind: 'step', step: before }, { kind: 'subagent', subagent: s }, { kind: 'step', step: after },
    ])
    // Not yet reached, with more pages to load.
    expect(timelineItems([before], [s], false)).toEqual([{ kind: 'step', step: before }])
    // The last page: at the end.
    expect(timelineItems([before], [s], true)).toEqual([{ kind: 'step', step: before }, { kind: 'subagent', subagent: s }])
  })
})

describe('fromText', () => {
  it('names the person, or says that a message is not from a person', () => {
    expect(fromText({ kind: 'person', name: 'Ada' })).toBe('Ada')
    expect(fromText({ kind: 'person', name: null })).toBe('Person')
    expect(fromText({ kind: 'not from a person' })).toBe('Not from a person')
    expect(fromText({ kind: 'agent' })).toBe('Agent')
  })
})

describe('timelinePath', () => {
  it('asks for the next page of a transcript', () => {
    expect(timelinePath('/acme/agent_sessions/x/timeline', { cursor: 'c2' })).toBe('/acme/agent_sessions/x/timeline?cursor=c2')
    expect(timelinePath('/acme/agent_sessions/x/timeline', { path: 'subagents/agent-a1.jsonl' }))
      .toBe('/acme/agent_sessions/x/timeline?path=subagents%2Fagent-a1.jsonl')
  })
})
