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

export interface TimelineOptions {
  path?: string
  cursor?: string | null
  /** Keeps the steps that have this text, with any case; with no path, from all transcripts. */
  search?: string
}

/** The URL of one page of a transcript's timeline, or of the steps that a search finds. */
export function timelinePath(base: string, { path, cursor, search }: TimelineOptions): string {
  const params = new URLSearchParams()
  if (path) params.set('path', path)
  if (cursor) params.set('cursor', cursor)
  if (search?.trim()) params.set('search', search.trim())
  const query = params.toString()
  return query ? `${base}?${query}` : base
}

/** Reads one page of a transcript's timeline from Console. */
export async function fetchTimelinePage(base: string, options: TimelineOptions): Promise<TimelinePage> {
  const response = await fetch(timelinePath(base, options), { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
  const body = await response.json().catch(() => null)
  if (!response.ok || !Array.isArray(body?.steps)) {
    throw new Error(body?.error ?? `The timeline could not load (${response.status}).`)
  }
  return body as TimelinePage
}

/** The raw line of a step, which 'Show full' reads. */
export interface RawLine {
  /** The line as stored, redacted. */
  line: string
  /** The raw layer does not hold all of the line. */
  cut: boolean
}

/** The URL of the raw line of a step, by its path, version and offset. */
export function linePath(base: string, step: Pick<TimelineStep, 'path' | 'version' | 'offset'>): string {
  const params = new URLSearchParams({ path: step.path, version: String(step.version), offset: String(step.offset) })
  return `${base}?${params}`
}

/** Reads the raw line of a step from Console. */
export async function fetchRawLine(base: string, step: Pick<TimelineStep, 'path' | 'version' | 'offset'>): Promise<RawLine> {
  const response = await fetch(linePath(base, step), { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
  const body = await response.json().catch(() => null)
  if (!response.ok || typeof body?.line !== 'string') {
    throw new Error(body?.error ?? `The full line could not load (${response.status}).`)
  }
  return body as RawLine
}

/** A raw line as 'Show full' shows it: a JSON line indented, any other text as it is. */
export function rawLineText(line: string): string {
  try {
    return JSON.stringify(JSON.parse(line), null, 2)
  } catch {
    return line
  }
}

export type ToolItem = Extract<TimelineItem, { kind: 'tool' }>

/** A timeline item as the page shows it: a run of tool calls is one group. */
export type ShownItem = TimelineItem | { kind: 'tools'; tools: ToolItem[] }

/** The shortest run of tool calls that folds into one group. */
export const GROUP_FROM = 3

/**
 * The items with each run of GROUP_FROM or more tool calls in one group, so
 * that the person messages and the agent replies are easy to find. A tool
 * call that started a subagent is never in a group, so the subagent shows.
 */
export function groupTools(items: TimelineItem[]): ShownItem[] {
  const shown: ShownItem[] = []
  let run: ToolItem[] = []
  const end = () => {
    if (run.length >= GROUP_FROM) shown.push({ kind: 'tools', tools: run })
    else shown.push(...run)
    run = []
  }
  for (const item of items) {
    if (item.kind === 'tool' && !item.subagent) {
      run.push(item)
      continue
    }
    end()
    shown.push(item)
  }
  end()
  return shown
}

const SUMMARY_KEYS = ['description', 'command', 'file_path', 'path', 'pattern', 'url', 'query', 'skill', 'prompt']

/**
 * One line that tells what a tool call did: the input's description, else
 * its command, file, pattern or other main value, else the input as it is.
 */
export function toolSummary(call: TimelineStep | null): string {
  if (!call) return ''
  let input: unknown = null
  try {
    input = JSON.parse(call.text)
  } catch {
    // A cut input is not whole JSON: show its start.
  }
  if (input && typeof input === 'object' && !Array.isArray(input)) {
    for (const key of SUMMARY_KEYS) {
      const value = (input as Record<string, unknown>)[key]
      if (typeof value === 'string' && value.trim()) return oneLine(value)
    }
  }
  return oneLine(call.text)
}

/** The tools of a group and how many calls each has, as "Bash 5, Read 4". */
export function toolCounts(tools: ToolItem[]): string {
  const counts = new Map<string, number>()
  for (const t of tools) {
    const name = (t.call ?? t.result)?.tool_name || 'Tool'
    counts.set(name, (counts.get(name) ?? 0) + 1)
  }
  return [...counts].map(([name, n]) => `${name} ${n}`).join(', ')
}

function oneLine(text: string): string {
  const line = text.replace(/\s+/g, ' ').trim()
  return line.length > 200 ? `${line.slice(0, 200)}…` : line
}

/** What the timeline shows besides the person messages and the agent replies. */
export interface TimelineShow {
  tools: boolean
  thinking: boolean
}

/** A step of the agent's thinking. */
export const isThinking = (s: TimelineStep) => s.line.endsWith('/thinking')

/**
 * The items that the choices show. With no tool calls, a tool call that
 * started a subagent stays, as the page shows it as that subagent.
 */
export function showItems(items: TimelineItem[], show: TimelineShow): TimelineItem[] {
  return items.filter(item => {
    if (item.kind === 'step') return show.thinking || !isThinking(item.step)
    if (item.kind === 'tool') return show.tools || !!item.subagent
    return true
  })
}

/** What the main agent asked of a subagent, from the tool call that started it. */
export interface SubagentBrief {
  description: string | null
  type: string | null
}

export function subagentBrief(call: TimelineStep | null): SubagentBrief {
  try {
    const input = JSON.parse(call?.text ?? '')
    const text = (v: unknown) => (typeof v === 'string' && v.trim() ? oneLine(v) : null)
    return { description: text(input?.description), type: text(input?.subagent_type) }
  } catch {
    return { description: null, type: null }
  }
}

/** How long from first to last, in words: "45 s", "13 min", "2 h 5 min". */
export function spanText(first: string, last: string): string {
  const seconds = Math.max(0, Math.round((Date.parse(last) - Date.parse(first)) / 1000))
  if (seconds < 60) return `${seconds} s`
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes} min`
  return `${Math.floor(minutes / 60)} h ${minutes % 60} min`
}

/**
 * A message's text as a person reads it: Claude Code's tags for a slash
 * command and its output become the command and the output, and its
 * <system-reminder> wrappers go.
 */
export function messageText(text: string): string {
  const tag = (name: string) => text.match(new RegExp(`<${name}>([\\s\\S]*?)</${name}>`))?.[1]?.trim()
  const command = tag('command-name')
  if (command !== undefined) {
    const args = tag('command-args')
    return [command, args].filter(Boolean).join(' ')
  }
  const out = tag('local-command-stdout') ?? tag('local-command-stderr')
  if (out !== undefined) return out
  return text.replace(/<\/?system-reminder>/g, '').trim()
}
