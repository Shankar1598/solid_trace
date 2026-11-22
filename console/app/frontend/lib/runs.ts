// The Runs page: its search (the range and the filters stay in the URL, so
// that a filtered view can be shared), and how a Run's state and actor
// read. The rows come from RunsView, one for each Run: its last run event.
import type { TimeRange } from './timeRange'

/** The states of a Run, as the run events name them (RunsView::STATES). */
export type RunState =
  | 'queued'
  | 'running'
  | 'waiting_on_person'
  | 'waiting_on_account_limit'
  | 'taken_over'
  | 'done'
  | 'failed'
  | 'stopped'

export interface RunFilters {
  /** The slug of the Project whose Factory settings the Run uses. */
  project: string | null
  state: RunState | null
}

export interface RunActor {
  /** person, sender, schedule, forge user or tracker user. */
  kind: string
  id: string
  name: string
}

export interface RunRow {
  id: string
  seq: number
  /** The time of the Run's last event. */
  time: string
  kind: string
  state: string
  reason: string
  project: string
  repository: string
  folder: string
  /** The branch that the Run starts from; empty is the repository's default branch. */
  base_branch?: string
  trigger: string
  title: string
  actor: RunActor
  pr: { url: string; outcome: string }
  executions: number
  active: boolean
  closed: boolean
}

export const WAITING_ON_PERSON: RunState = 'waiting_on_person'

const STATE_TEXT: Record<RunState, string> = {
  queued: 'Queued',
  running: 'Running',
  waiting_on_person: 'Waiting on a person',
  waiting_on_account_limit: 'Waiting on an account limit',
  taken_over: 'Taken over',
  done: 'Done',
  failed: 'Failed',
  stopped: 'Stopped',
}

type Tone = 'neutral' | 'info' | 'warn' | 'error' | 'ok'

const STATE_TONE: Record<RunState, Tone> = {
  queued: 'info',
  running: 'info',
  waiting_on_person: 'warn',
  waiting_on_account_limit: 'warn',
  taken_over: 'info',
  done: 'ok',
  failed: 'error',
  stopped: 'neutral',
}

function isRunState(state: string): state is RunState {
  return Object.hasOwn(STATE_TEXT, state)
}

/** A state's name in sentence case; a state this page does not know stays as it is. */
export function stateText(state: string): string {
  return isRunState(state) ? STATE_TEXT[state] : state
}

/** The Badge tone of a state: a wait for a person or a limit warns, a failure is an error. */
export function stateTone(state: string): Tone {
  return isRunState(state) ? STATE_TONE[state] : 'neutral'
}

/** Who started the Run: the actor's name, else its kind and id. */
export function actorText(actor: RunActor): string {
  if (actor.name) return actor.name
  const text = [actor.kind, actor.id].filter(Boolean).join(' ')
  return text || 'Unknown'
}

/** The one-click filter 'Waiting on a person': on, or off again when it is on. The Project filter stays. */
export function waitingOnPersonFilters(filters: RunFilters): RunFilters {
  return { ...filters, state: filters.state === WAITING_ON_PERSON ? null : WAITING_ON_PERSON }
}

/** An Execution: one time that a Run works on a VM. */
export interface RunExecution {
  id: string
  start: string | null
  end: string | null
  reason: string | null
  exit_code: number | null
  account: string | null
  vm: string | null
  /** headless, or interactive for the Execution of a take-over. */
  form: string
  /** The command that attaches to a take-over's terminal. */
  attach: string | null
  duration_seconds: number | null
  /** Started, not ended, and its machine is not lost. */
  live: boolean
}

/** The take-over of a Run that is taken over. */
export interface RunTakeOver {
  vm: string | null
  attach: string | null
  by: { kind: string; id: string; name: string }
}

/** One run event, in run.seq order. */
export interface RunEvent {
  seq: number
  time: string
  kind: string
  state: string
  reason: string | null
  actor: RunActor
  intervention: boolean
  detail: string | null
}

/** A check of a report: its command, exit status, time and the last lines of its output. */
export interface RunCheck {
  name: string
  command: string
  exit_status: number | null
  passed: boolean
  time: string
  output_tail: string
}

/** The last report of a Run. */
export interface RunReport {
  outcome: string
  summary: string | null
  question: string | null
  cause_report: string | null
  checks: RunCheck[]
  evidence: { path: string; url: string; image: boolean }[]
  no_evidence: boolean
  report_md: string | null
  report_md_error: string | null
}

/** How long an Execution took; 'Live' while it runs, and '-' when it has no end (as a lost machine). */
export function executionDurationText(e: Pick<RunExecution, 'duration_seconds' | 'live'>): string {
  if (e.live) return 'Live'
  return e.duration_seconds === null ? '-' : durationText(e.duration_seconds)
}

/** How long, in seconds, as 1h 2m. */
export function durationText(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`
  if (m > 0) return s > 0 ? `${m}m ${s}s` : `${m}m`
  return `${s}s`
}

const KIND_TEXT: Record<string, string> = { take_over: 'Take-over', pr_opened: 'PR opened', pr_outcome: 'PR outcome' }

/** A run event's kind in sentence case. */
export function eventKindText(kind: string): string {
  if (Object.hasOwn(KIND_TEXT, kind)) return KIND_TEXT[kind]
  const text = kind.replaceAll('_', ' ')
  return text.charAt(0).toUpperCase() + text.slice(1)
}

/** An event's detail: a report's outcome (needs_a_person) in sentence case; other details as they are. */
export function eventDetailText(kind: string, detail: string): string {
  return kind === 'report' ? eventKindText(detail) : detail
}

/** A check's result: pass, or fail with its exit status. */
export function checkText(check: Pick<RunCheck, 'passed' | 'exit_status'>): string {
  if (check.passed) return 'Pass'
  return check.exit_status === null ? 'Fail' : `Fail (exit status ${check.exit_status})`
}

/** The page's search for a range and filters: a span stays a span, and a filter that is not set is left out. */
export function runsSearch(range: TimeRange, filters: RunFilters): string {
  const params = new URLSearchParams()
  if (range.kind === 'relative') {
    params.set('range', range.span)
  } else {
    params.set('from', range.from.toISOString())
    params.set('to', range.to.toISOString())
  }
  for (const name of ['project', 'state'] as const) {
    const value = filters[name]
    if (value) params.set(name, value)
  }
  return `?${params}`
}
