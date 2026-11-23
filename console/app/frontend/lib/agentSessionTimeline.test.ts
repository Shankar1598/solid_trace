import { describe, expect, it } from 'vitest'
import { addPage, commandOutput, compactionSummaryText, fromText, groupTools, inOrder, isCompactionSummary, linePath, messageText, rawLineText, showItems, spanText, stripAnsi, subagentBrief, timelineItems, timelinePath, toolCounts, toolSummary, type ShownItem, type TimelineItem, type TimelineStep, type TimelineSubagent, type ToolItem } from './agentSessionTimeline'

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

describe('the order of the timeline', () => {
  const one = step('person', 'One', { time: '2026-10-02T11:00:01Z' })
  const two = step('assistant', 'Two', { line: 'assistant/text', time: '2026-10-02T11:00:02Z' })
  const three = step('person', 'Three', { time: '2026-10-02T11:00:03Z' })
  const four = step('assistant', 'Four', { line: 'assistant/text', time: '2026-10-02T11:00:04Z' })

  it('keeps the loaded steps in time order: a page of the oldest order goes after them, of the newest order before them', () => {
    expect(addPage([one, two], [three, four], 'oldest')).toEqual([one, two, three, four])
    expect(addPage([three, four], [two, one], 'newest')).toEqual([one, two, three, four])
  })

  it('shows the newest item first in the newest order, also inside a group of tool calls', () => {
    const tool = (id: string): ToolItem => ({ kind: 'tool', call: step('assistant', '{}', { line: 'assistant/tool_use', tool_name: 'Read', tool_use_id: id }), result: null, subagent: null })
    const [a, b, c] = [tool('A'), tool('B'), tool('C')]
    const person = { kind: 'step' as const, step: one }
    const shown: ShownItem[] = [person, { kind: 'tools', tools: [a, b, c] }]

    expect(inOrder(shown, 'oldest')).toEqual(shown)
    expect(inOrder(shown, 'newest')).toEqual([{ kind: 'tools', tools: [c, b, a] }, person])
  })

  it('in the newest order, puts a subagent that no call names only where the loaded steps reach it', () => {
    const s = subagent({ tool_use_id: '', first: '2026-10-02T11:00:02.500Z' })
    const late = subagent({ tool_use_id: '', path: 'subagents/agent-a2.jsonl', agent_id: 'a2', first: '2026-10-02T11:00:05Z' })

    // The newest page holds three and four: the older subagent waits for
    // its page, and the subagent after the last step shows at the end.
    expect(timelineItems([three, four], [s, late], false, 'newest')).toEqual([
      { kind: 'step', step: three }, { kind: 'step', step: four }, { kind: 'subagent', subagent: late },
    ])
    // All pages: each at its first time.
    expect(timelineItems([one, two, three, four], [s, late], true, 'newest')).toEqual([
      { kind: 'step', step: one }, { kind: 'step', step: two }, { kind: 'subagent', subagent: s },
      { kind: 'step', step: three }, { kind: 'step', step: four }, { kind: 'subagent', subagent: late },
    ])
  })

  it('asks EventStore for the newest order, and nothing for the oldest, its default', () => {
    expect(timelinePath('/acme/agent_sessions/x/timeline', { order: 'newest', cursor: 'c2' })).toBe('/acme/agent_sessions/x/timeline?cursor=c2&order=newest')
    expect(timelinePath('/acme/agent_sessions/x/timeline', { order: 'oldest' })).toBe('/acme/agent_sessions/x/timeline')
  })
})

describe('timelinePath', () => {
  it('asks for the next page of a transcript', () => {
    expect(timelinePath('/acme/agent_sessions/x/timeline', { cursor: 'c2' })).toBe('/acme/agent_sessions/x/timeline?cursor=c2')
    expect(timelinePath('/acme/agent_sessions/x/timeline', { path: 'subagents/agent-a1.jsonl' }))
      .toBe('/acme/agent_sessions/x/timeline?path=subagents%2Fagent-a1.jsonl')
  })
})

describe('the search inside an Agent session', () => {
  it('asks for the steps that have the text, page by page', () => {
    expect(timelinePath('/acme/agent_sessions/x/timeline', { search: 'cart page', cursor: 'c2' }))
      .toBe('/acme/agent_sessions/x/timeline?cursor=c2&search=cart+page')
    expect(timelinePath('/acme/agent_sessions/x/timeline', { search: '  ' })).toBe('/acme/agent_sessions/x/timeline')
  })
})

describe("'Show full'", () => {
  it('asks for the raw line of the step', () => {
    expect(linePath('/acme/agent_sessions/x/line', { path: 'subagents/agent-a1.jsonl', version: 2, offset: 120 }))
      .toBe('/acme/agent_sessions/x/line?path=subagents%2Fagent-a1.jsonl&version=2&offset=120')
  })

  it('shows a JSON line indented, and any other text as it is', () => {
    expect(rawLineText('{"type":"user","message":{"content":"Hi"}}'))
      .toBe('{\n  "type": "user",\n  "message": {\n    "content": "Hi"\n  }\n}')
    expect(rawLineText('{"type":"user", cut')).toBe('{"type":"user", cut')
  })
})

describe('groupTools', () => {
  const tool = (name: string, more: Partial<Extract<TimelineItem, { kind: 'tool' }>> = {}): Extract<TimelineItem, { kind: 'tool' }> => ({
    kind: 'tool', call: step('assistant', '{}', { line: 'assistant/tool_use', tool_name: name, tool_use_id: `t${offset}` }), result: null, subagent: null, ...more,
  })

  it('folds a run of three or more tool calls into one group, and keeps a shorter run as it is', () => {
    const person = { kind: 'step' as const, step: step('person', 'Fix it') }
    const reply = { kind: 'step' as const, step: step('assistant', 'Done.', { line: 'assistant/text' }) }
    const [a, b, c, d, e] = [tool('Bash'), tool('Read'), tool('Bash'), tool('Edit'), tool('Edit')]

    expect(groupTools([person, a, b, c, reply, d, e])).toEqual([person, { kind: 'tools', tools: [a, b, c] }, reply, d, e])
  })

  it('never puts a tool call that started a subagent in a group', () => {
    const [a, b] = [tool('Read'), tool('Read')]
    const agent = tool('Agent', { subagent: subagent() })
    const [c, d] = [tool('Grep'), tool('Grep')]

    expect(groupTools([a, b, agent, c, d])).toEqual([a, b, agent, c, d])
  })

  it('counts the calls of each tool in a group, in the order they came', () => {
    expect(toolCounts([tool('Bash'), tool('Read'), tool('Bash')])).toBe('Bash 2, Read 1')
  })
})

describe('toolSummary', () => {
  const call = (text: string) => step('assistant', text, { line: 'assistant/tool_use', tool_name: 'Bash', tool_use_id: 'x' })

  it('shows the description of a tool call, else its main value', () => {
    expect(toolSummary(call('{"command":"ls -la","description":"List files"}'))).toBe('List files')
    expect(toolSummary(call('{"file_path":"/app/cart.go","limit":40}'))).toBe('/app/cart.go')
    expect(toolSummary(call('{"command":"go test\\n  ./..."}'))).toBe('go test ./...')
  })

  it('shows the start of an input that is not whole JSON', () => {
    expect(toolSummary(call('{"command":"cat a.txt'))).toBe('{"command":"cat a.txt')
    expect(toolSummary(null)).toBe('')
  })
})

describe('showItems', () => {
  const person = { kind: 'step' as const, step: step('person', 'Fix it') }
  const thinking = { kind: 'step' as const, step: step('assistant', 'Hmm.', { line: 'assistant/thinking' }) }
  const reply = { kind: 'step' as const, step: step('assistant', 'Done.', { line: 'assistant/text' }) }
  const read = { kind: 'tool' as const, call: step('assistant', '{}', { line: 'assistant/tool_use', tool_name: 'Read', tool_use_id: 'r' }), result: null, subagent: null }
  const s = subagent()
  const agent = { kind: 'tool' as const, call: step('assistant', '{}', { line: 'assistant/tool_use', tool_name: 'Agent', tool_use_id: 'toolu_9' }), result: null, subagent: s }
  const items = [person, thinking, read, agent, reply]

  it('shows each item when both are on', () => {
    expect(showItems(items, { tools: true, thinking: true })).toEqual(items)
  })

  it('hides the thinking, and the tool calls but not one that started a subagent', () => {
    expect(showItems(items, { tools: false, thinking: false })).toEqual([person, agent, reply])
  })
})

describe('subagentBrief', () => {
  it('reads the description and the type of the subagent from the tool call', () => {
    const call = step('assistant', '{"description":"Audit v2 search","subagent_type":"general-purpose","prompt":"Read it."}', { line: 'assistant/tool_use', tool_name: 'Agent', tool_use_id: 'x' })
    expect(subagentBrief(call)).toEqual({ description: 'Audit v2 search', type: 'general-purpose' })
    expect(subagentBrief(null)).toEqual({ description: null, type: null })
  })
})

describe('spanText', () => {
  it('tells how long a subagent ran', () => {
    expect(spanText('2026-10-02T11:00:00Z', '2026-10-02T11:00:45Z')).toBe('45 s')
    expect(spanText('2026-10-02T11:01:10Z', '2026-10-02T11:14:41Z')).toBe('14 min')
    expect(spanText('2026-10-02T11:00:00Z', '2026-10-02T13:05:00Z')).toBe('2 h 5 min')
  })
})

describe('messageText', () => {
  it('shows a slash command and its output as a person reads them', () => {
    expect(messageText('<command-name>/model</command-name>\n  <command-message>model</command-message>\n  <command-args></command-args>')).toBe('/model')
    expect(messageText('<command-name>/review</command-name><command-args>main</command-args>')).toBe('/review main')
    expect(messageText('<local-command-stdout>Kept model as `Opus`</local-command-stdout>')).toBe('Kept model as `Opus`')
  })

  it('removes the system-reminder wrappers, and keeps other text as it is', () => {
    expect(messageText('<system-reminder>\nCall the tool.\n</system-reminder>')).toBe('Call the tool.')
    expect(messageText('Fix the <b>cart</b>.')).toBe('Fix the <b>cart</b>.')
  })

  it("removes the terminal's colour codes from a command's output", () => {
    expect(messageText('<local-command-stdout>\u001b[2mCompacted (ctrl+o to see full summary)\u001b[22m</local-command-stdout>'))
      .toBe('Compacted (ctrl+o to see full summary)')
    expect(messageText('<local-command-stderr>\u001b[31;1mError:\u001b[0m no model</local-command-stderr>')).toBe('Error: no model')
  })

  it("gives no text for Claude Code's caveat before a command, so that it does not show", () => {
    expect(messageText('<local-command-caveat>The command below was run directly in Claude Code, not by you.</local-command-caveat>')).toBe('')
    expect(messageText('<local-command-caveat>Caveat: the messages below were made by local commands.</local-command-caveat>')).toBe('')
  })
})

describe('stripAnsi', () => {
  it('removes the escape sequences of a terminal and keeps the text', () => {
    expect(stripAnsi('\u001b[2mdim\u001b[22m, \u001b[38;5;246mgrey\u001b[39m, \u001b[?25lcursor\u001b[K')).toBe('dim, grey, cursor')
    expect(stripAnsi('\u001b]8;;https://example.com\u0007link\u001b]8;;\u0007')).toBe('link')
    expect(stripAnsi('No codes [2m here')).toBe('No codes [2m here')
  })
})

describe('commandOutput', () => {
  it("is the output of a slash command, also from a parse that gave it the person's role", () => {
    expect(commandOutput(step('person', '<local-command-stdout>\u001b[2mCompacted\u001b[22m</local-command-stdout>'))).toBe('Compacted')
    expect(commandOutput(step('not from a person', '<local-command-stderr>No model</local-command-stderr>'))).toBe('No model')
    expect(commandOutput(step('person', 'Compacted'))).toBeNull()
  })
})

describe('a slash command in the timeline', () => {
  // The lines that Claude Code writes for /compact, in time order: the
  // input, the caveat and the command at the same second, the summary,
  // and the output when the compaction ends.
  const compact = () => {
    const input = step('person', '/compact', { time: '2026-11-11T15:00:18.522Z' })
    const caveat = step('not from a person', '<local-command-caveat>The command below was run directly in Claude Code.</local-command-caveat>', { time: '2026-11-11T15:00:18.532Z' })
    const command = step('person', '<command-name>/compact</command-name>\n  <command-message>compact</command-message>\n  <command-args></command-args>', { time: '2026-11-11T15:00:18.532Z' })
    const summary = step('not from a person', 'This session is being continued from a previous conversation that ran out of context.', { time: '2026-11-11T15:00:54Z' })
    const boundary = step('system', 'Conversation compacted', { line: 'system/compact_boundary', time: '2026-11-11T15:00:54.379Z' })
    const output = step('not from a person', '<local-command-stdout>\u001b[2mCompacted (ctrl+o to see full summary)\u001b[22m</local-command-stdout>', { time: '2026-11-11T15:00:54.478Z' })
    return { input, caveat, command, summary, boundary, output }
  }

  it('is one command step, with its output inside it', () => {
    const { input, caveat, command, summary, boundary, output } = compact()
    expect(timelineItems([input, caveat, command, summary, boundary, output], [], true)).toEqual([
      { kind: 'step', step: caveat },
      { kind: 'step', step: command, output },
      { kind: 'step', step: summary },
      { kind: 'step', step: boundary },
    ])
  })

  it('stays one step in the newest order, with its output inside it', () => {
    const { input, caveat, command, summary, boundary, output } = compact()
    const items = inOrder(timelineItems([input, caveat, command, summary, boundary, output], [], true, 'newest'), 'newest')
    expect(items.map(i => i.kind === 'step' ? i.step.id : '')).toEqual([boundary.id, summary.id, command.id, caveat.id])
    expect(items[2]).toEqual({ kind: 'step', step: command, output })
  })

  it('keeps an output from a parse that gave it the person\'s role inside its command', () => {
    const { command } = compact()
    const output = step('person', '<local-command-stdout>Set model to Opus</local-command-stdout>', { time: '2026-11-11T15:00:19Z' })
    expect(timelineItems([command, output], [], true)).toEqual([{ kind: 'step', step: command, output }])
  })

  it('does not join an output to a command before a person message or an agent reply', () => {
    const { command, output } = compact()
    const reply = step('assistant', 'Done.', { line: 'assistant/text', time: '2026-11-11T15:00:20Z' })
    expect(timelineItems([command, reply, output], [], true)).toEqual([
      { kind: 'step', step: command },
      { kind: 'step', step: reply },
      { kind: 'step', step: output },
    ])
  })

  it('keeps a person message that only looks like the command when it is not just before it', () => {
    const { command } = compact()
    const typed = step('person', '/compact', { time: '2026-11-11T14:00:00Z' })
    const reply = step('assistant', 'Use /compact.', { line: 'assistant/text', time: '2026-11-11T14:00:05Z' })
    expect(timelineItems([typed, reply, command], [], true).map(i => i.kind === 'step' && i.step.id))
      .toEqual([typed.id, reply.id, command.id])
  })

  it('takes the input with its arguments as the same command', () => {
    const input = step('person', '/review the cart PR', { time: '2026-11-11T15:00:18Z' })
    const command = step('person', '<command-name>/review</command-name><command-args>the cart PR</command-args>', { time: '2026-11-11T15:00:18.1Z' })
    expect(timelineItems([input, command], [], true)).toEqual([{ kind: 'step', step: command }])
  })
})

describe('the compaction summary', () => {
  it('is the summary that Claude Code writes when it compacts the conversation', () => {
    expect(isCompactionSummary(step('not from a person', 'This session is being continued from a previous conversation that ran out of context.'))).toBe(true)
    expect(isCompactionSummary(step('person', 'This session is being continued from a previous conversation, said the person.'))).toBe(false)
    expect(isCompactionSummary(step('not from a person', 'Stop hook feedback'))).toBe(false)
  })

  it("shows Claude Code's tags as headings", () => {
    const text = 'This session is being continued.\n<tools-used-before-summary>Read, Bash</tools-used-before-summary>\n<system-reminder>Plan</system-reminder>'
    const shown = compactionSummaryText(text)
    expect(shown).not.toMatch(/<\/?[a-z-]+>/)
    expect(shown).toContain('**Tools used before summary**')
    expect(shown).toContain('Read, Bash')
    expect(shown).toContain('Plan')
  })
})
