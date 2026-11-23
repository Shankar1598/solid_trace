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

/**
 * The order in which the page shows a timeline: the oldest step first, as
 * a chat reads, or the newest first. In the newest order the first page is
 * the end of the timeline, and each next page is older.
 */
export type TimelineOrder = 'oldest' | 'newest'

export type TimelineItem =
  /** A message. A slash command has its output in it, when the output is loaded. */
  | { kind: 'step'; step: TimelineStep; output?: TimelineStep }
  | { kind: 'tool'; call: TimelineStep | null; result: TimelineStep | null; subagent: TimelineSubagent | null }
  | { kind: 'subagent'; subagent: TimelineSubagent }

const isCall = (s: TimelineStep) => s.line.endsWith('tool_use') && !!s.tool_use_id
const isResult = (s: TimelineStep) => s.role === 'tool'

/**
 * The items of the steps loaded so far, which are in time order (addPage).
 * A subagent that no tool call names shows at its first time, when the
 * steps reach it, or at the end when the end is loaded. In the oldest
 * order the end is loaded when `done` (no page is left); in the newest
 * order the end is always loaded, and a subagent before the first loaded
 * step waits until its page loads, or `done`.
 */
export function timelineItems(steps: TimelineStep[], subagents: TimelineSubagent[], done: boolean, order: TimelineOrder = 'oldest'): TimelineItem[] {
  const calls = new Set(steps.filter(isCall).map(s => s.tool_use_id))
  const results = new Map<string, TimelineStep>()
  for (const s of steps) if (isResult(s) && s.tool_use_id && calls.has(s.tool_use_id)) results.set(s.tool_use_id, s)
  const byCall = new Map(subagents.filter(s => s.tool_use_id).map(s => [s.tool_use_id, s]))
  const byAgent = new Map(subagents.map(s => [s.agent_id, s]))
  let loose = subagents.filter(s => !s.tool_use_id).sort((a, b) => a.first.localeCompare(b.first))
  if (order === 'newest' && !done) {
    const start = steps.length > 0 ? Date.parse(steps[0].time) : Infinity
    loose = loose.filter(s => Date.parse(s.first) >= start)
  }

  const items: TimelineItem[] = []
  const placeLoose = (before: string | null) => {
    while (loose.length > 0 && (before === null || Date.parse(loose[0].first) < Date.parse(before))) {
      items.push({ kind: 'subagent', subagent: loose.shift()! })
    }
  }
  const inputs = commandInputs(steps)
  // The last slash command that its output can join: until a person
  // message, an agent's step or a tool call comes after it.
  let command: Extract<TimelineItem, { kind: 'step' }> | null = null
  for (const s of steps) {
    if (inputs.has(s.id)) continue
    placeLoose(s.time)
    if (isCall(s)) {
      command = null
      const result = results.get(s.tool_use_id!) ?? null
      const subagent = byCall.get(s.tool_use_id!) ?? (result?.subagent_id ? byAgent.get(result.subagent_id) ?? null : null)
      items.push({ kind: 'tool', call: s, result, subagent })
    } else if (isResult(s)) {
      command = null
      if (s.tool_use_id && calls.has(s.tool_use_id)) continue
      items.push({ kind: 'tool', call: null, result: s, subagent: s.subagent_id ? byAgent.get(s.subagent_id) ?? null : null })
    } else if (commandOutput(s) !== null && command) {
      command.output = s
      command = null
    } else {
      const item: TimelineItem = { kind: 'step', step: s }
      items.push(item)
      if (isCommand(s)) command = item
      else if (commandOutput(s) === null && (s.from.kind === 'person' || s.from.kind === 'agent')) command = null
    }
  }
  if (done || order === 'newest') placeLoose(null)
  return items
}

/**
 * The steps loaded so far with the steps of one more page, in time order:
 * a page of the oldest order comes after them, and a page of the newest
 * order, which is newest first, comes before them.
 */
export function addPage(steps: TimelineStep[], page: TimelineStep[], order: TimelineOrder): TimelineStep[] {
  return order === 'oldest' ? [...steps, ...page] : [...[...page].reverse(), ...steps]
}

/**
 * The shown items, which are in time order, in the order of the page: in
 * the newest order the newest item first, also inside a group of tool
 * calls. A tool call keeps its input before its output.
 */
export function inOrder(shown: ShownItem[], order: TimelineOrder): ShownItem[] {
  if (order === 'oldest') return shown
  return [...shown].reverse().map(item => item.kind === 'tools' ? { ...item, tools: [...item.tools].reverse() } : item)
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
  /** The oldest order is EventStore's default, so only the newest is sent. */
  order?: TimelineOrder
  /** Keeps the steps that have this text, with any case; with no path, from all transcripts. */
  search?: string
}

/** The URL of one page of a transcript's timeline, or of the steps that a search finds. */
export function timelinePath(base: string, { path, cursor, search, order }: TimelineOptions): string {
  const params = new URLSearchParams()
  if (path) params.set('path', path)
  if (cursor) params.set('cursor', cursor)
  if (search?.trim()) params.set('search', search.trim())
  if (order === 'newest') params.set('order', order)
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

/** The text in the first <name>…</name> of text, trimmed; undefined for none. */
function tagText(text: string, name: string): string | undefined {
  return text.match(new RegExp(`<${name}>([\\s\\S]*?)</${name}>`))?.[1]?.trim()
}

/**
 * A message's text as a person reads it: Claude Code's tags for a slash
 * command and its output become the command and the output, with no
 * colour codes of the terminal, and its <system-reminder> wrappers go.
 * Claude Code's caveat before a command is for the model, so it gives ''
 * and does not show.
 */
export function messageText(text: string): string {
  const command = tagText(text, 'command-name')
  if (command !== undefined) {
    const args = tagText(text, 'command-args')
    return [command, args].filter(Boolean).join(' ')
  }
  const out = tagText(text, 'local-command-stdout') ?? tagText(text, 'local-command-stderr')
  if (out !== undefined) return stripAnsi(out).trim()
  if (tagText(text, 'local-command-caveat') !== undefined) return ''
  return text.replace(/<\/?system-reminder>/g, '').trim()
}

/**
 * The text with no escape sequences of a terminal: the colours and styles
 * (CSI, as ESC[2m), the links and titles (OSC) and the other two-character
 * escapes. Claude Code keeps them in a command's output.
 */
export function stripAnsi(text: string): string {
  return text
    .replace(/\u001b\][^\u0007\u001b]*(?:\u0007|\u001b\\)/g, '')
    .replace(/\u001b\[[0-?]*[ -/]*[@-~]/g, '')
    .replace(/\u001b[@-Z\\-_]/g, '')
}

/** A slash command that a person gave: Claude Code's <command-name> line. */
export const isCommand = (s: TimelineStep) => s.text.includes('<command-name>')

/**
 * The output of a slash command, as a person reads it, or null for a step
 * that is not one. A parse before v0.23.3 gave the output the person's
 * role, so the text decides.
 */
export function commandOutput(s: TimelineStep): string | null {
  const out = tagText(s.text, 'local-command-stdout') ?? tagText(s.text, 'local-command-stderr')
  return out === undefined ? null : stripAnsi(out).trim()
}

/** How far the input of a slash command can be before its <command-name> line. */
const INPUT_BEFORE_COMMAND_MS = 10_000

/**
 * The ids of the steps that are the input of a slash command: Claude Code
 * writes what the person typed (`/compact`) and then the command in its
 * tags, so the command shows once. The input is a person message that is
 * the same command, at most INPUT_BEFORE_COMMAND_MS before the command,
 * with no person message, agent step or tool call between them.
 */
function commandInputs(steps: TimelineStep[]): Set<string> {
  const inputs = new Set<string>()
  const words = (text: string) => text.replace(/\s+/g, ' ').trim()
  steps.forEach((s, i) => {
    if (s.from.kind !== 'person' || isCommand(s) || commandOutput(s) !== null || !s.text.trim().startsWith('/')) return
    for (const next of steps.slice(i + 1)) {
      if (Date.parse(next.time) - Date.parse(s.time) > INPUT_BEFORE_COMMAND_MS) return
      if (isCommand(next)) {
        if (messageText(next.text) === words(s.text)) inputs.add(s.id)
        return
      }
      if (commandOutput(next) === null && (next.from.kind === 'person' || next.from.kind === 'agent' || next.from.kind === 'tool')) return
    }
  })
  return inputs
}

const COMPACTION_SUMMARY = 'This session is being continued from a previous conversation'

/** The summary that Claude Code writes as the first message after it compacts the conversation. */
export const isCompactionSummary = (s: TimelineStep) => s.from.kind !== 'person' && s.text.startsWith(COMPACTION_SUMMARY)

/**
 * The compaction summary as Markdown: each of Claude Code's tags in it, as
 * <tools-used-before-summary>, is a heading of its words, and its
 * <system-reminder> wrappers go.
 */
export function compactionSummaryText(text: string): string {
  return text
    .replace(/<\/?system-reminder>/g, '')
    .replace(/<([a-z]+(?:-[a-z]+)+)>/g, (_, name: string) => {
      const words = name.replace(/-/g, ' ')
      return `\n\n**${words[0].toUpperCase()}${words.slice(1)}**\n\n`
    })
    .replace(/<\/[a-z]+(?:-[a-z]+)+>/g, '\n')
    .trim()
}
