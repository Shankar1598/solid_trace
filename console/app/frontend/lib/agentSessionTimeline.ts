// The Agent session page's timeline: the steps that AgentSessionView
// gives, page by page, put in the order that the page shows them. A tool's
// output goes inside its tool call, and a subagent inside the tool call that
// started it.

export interface StepFrom {
  /** person, agent, tool, not from a person, or system. */
  kind: string
  /** The person, for a person message whose author the page knows. */
  name?: string | null
}

export interface TimelineStep {
  id: string
  time: string
  path: string
  version: number
  offset: number
  /** The line's kind, as assistant/tool_use. */
  line: string
  role: string
  /** The message; for a tool call, its input as JSON. */
  text: string
  tool_name?: string
  tool_use_id?: string
  subagent_id?: string
  model?: string
  level?: string
  tokens: { input: number; output: number; cache_read: number; cache_write: number } | null
  cost_usd: number | null
  /** The text was cut at the Log limits; the raw line has all of it. */
  cut: boolean
  from: StepFrom
}

export interface TimelineSubagent {
  path: string
  agent_id: string
  /** The tool call that started it; '' when no result names it. */
  tool_use_id: string
  first: string
  last: string
}

export interface TimelinePage {
  steps: TimelineStep[]
  next_cursor: string | null
}

export type TimelineItem =
  | { kind: 'step'; step: TimelineStep }
  | { kind: 'tool'; call: TimelineStep | null; result: TimelineStep | null; subagent: TimelineSubagent | null }
  | { kind: 'subagent'; subagent: TimelineSubagent }

const isCall = (s: TimelineStep) => s.line.endsWith('tool_use') && !!s.tool_use_id
const isResult = (s: TimelineStep) => s.role === 'tool'

/**
 * The items of the steps loaded so far. A subagent that no tool call names
 * shows at its first time, when the steps reach it, or at the end when
 * `done` (no page is left).
 */
export function timelineItems(steps: TimelineStep[], subagents: TimelineSubagent[], done: boolean): TimelineItem[] {
  const calls = new Set(steps.filter(isCall).map(s => s.tool_use_id))
  const results = new Map<string, TimelineStep>()
  for (const s of steps) if (isResult(s) && s.tool_use_id && calls.has(s.tool_use_id)) results.set(s.tool_use_id, s)
  const byCall = new Map(subagents.filter(s => s.tool_use_id).map(s => [s.tool_use_id, s]))
  const byAgent = new Map(subagents.map(s => [s.agent_id, s]))
  const loose = subagents.filter(s => !s.tool_use_id).sort((a, b) => a.first.localeCompare(b.first))

  const items: TimelineItem[] = []
  const placeLoose = (before: string | null) => {
    while (loose.length > 0 && (before === null || Date.parse(loose[0].first) < Date.parse(before))) {
      items.push({ kind: 'subagent', subagent: loose.shift()! })
    }
  }
  for (const s of steps) {
    placeLoose(s.time)
    if (isCall(s)) {
      const result = results.get(s.tool_use_id!) ?? null
      const subagent = byCall.get(s.tool_use_id!) ?? (result?.subagent_id ? byAgent.get(result.subagent_id) ?? null : null)
      items.push({ kind: 'tool', call: s, result, subagent })
    } else if (isResult(s)) {
      if (s.tool_use_id && calls.has(s.tool_use_id)) continue
      items.push({ kind: 'tool', call: null, result: s, subagent: s.subagent_id ? byAgent.get(s.subagent_id) ?? null : null })
    } else {
      items.push({ kind: 'step', step: s })
    }
  }
  if (done) placeLoose(null)
  return items
}

const KIND_TEXT: Record<string, string> = {
  person: 'Person', agent: 'Agent', tool: 'Tool', 'not from a person': 'Not from a person', system: 'System',
}

/** Whom a step is from, as the timeline shows it. */
export function fromText(from: StepFrom): string {
  if (from.kind === 'person' && from.name) return from.name
  return KIND_TEXT[from.kind] ?? from.kind
}

/** The URL of one page of a transcript's timeline. */
export function timelinePath(base: string, { path, cursor }: { path?: string; cursor?: string | null }): string {
  const params = new URLSearchParams()
  if (path) params.set('path', path)
  if (cursor) params.set('cursor', cursor)
  const query = params.toString()
  return query ? `${base}?${query}` : base
}

/** Reads one page of a transcript's timeline from Console. */
export async function fetchTimelinePage(base: string, options: { path?: string; cursor?: string | null }): Promise<TimelinePage> {
  const response = await fetch(timelinePath(base, options), { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
  const body = await response.json().catch(() => null)
  if (!response.ok || !Array.isArray(body?.steps)) {
    throw new Error(body?.error ?? `The timeline could not load (${response.status}).`)
  }
  return body as TimelinePage
}
