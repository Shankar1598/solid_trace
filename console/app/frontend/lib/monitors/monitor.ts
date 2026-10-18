// Monitors as their pages know them, and what the form works out before a
// Monitor is saved: the groups a run would fire now, read from a table as
// MonitorEvaluation reads it, and the Queries its preview chart runs.
import type { Builder } from '@/lib/logs/builder'
import type { LogTable } from '@/lib/logs/table'
import { rangeMs, type TimeRange } from '@/lib/timeRange'

export type MonitorKind = 'threshold' | 'match'
export type MonitorOperator = '>' | '>=' | '<' | '<='
/** disabled when off, error when its last run failed, firing when any group is, ok otherwise. */
export type MonitorStatus = 'firing' | 'ok' | 'disabled' | 'error'

export const OPERATORS: MonitorOperator[] = ['>', '>=', '<', '<=']
/** A Monitor's bounds, as LogMonitor checks them. */
export const MAX_INTERVAL_MINUTES = 24 * 60
export const MAX_RANGE_MINUTES = 7 * 24 * 60

/** What the form edits and saves. */
export interface MonitorSettings {
  name: string
  kind: MonitorKind
  project_id: number | null
  query: string
  interval_minutes: number
  range_minutes: number
  operator: MonitorOperator | null
  threshold: number | null
  notify_by_group: boolean
  alert_on_no_data: boolean
  integration_ids: number[]
}

export interface MonitorGroupState {
  group_key: string
  group: Record<string, unknown>
  status: 'ok' | 'firing'
  since: string
  last_value: number | null
}

export interface MonitorRun {
  id: number
  ran_at: string
  status: 'ok' | 'error'
  error: string | null
  firing: number | null
  matched: number | null
  logs_skipped: boolean
  elapsed_ms: number | null
}

export interface IntegrationOption {
  id: number
  name: string
  provider: string
  enabled: boolean
}

export interface Monitor extends MonitorSettings {
  id: number
  project: { id: number; name: string; slug: string }
  enabled: boolean
  status: MonitorStatus
  firing_count: number
  last_run_at: string | null
  last_error: string | null
  logs_skipped_at: string | null
  created_at: string
  updated_at: string
  /** On the Monitor's own page. */
  states?: MonitorGroupState[]
  runs?: MonitorRun[]
  integrations?: IntegrationOption[]
}

/** Whether value passes the threshold, by the operator. */
export function breached(value: number, operator: MonitorOperator, threshold: number): boolean {
  switch (operator) {
    case '>': return value > threshold
    case '>=': return value >= threshold
    case '<': return value < threshold
    case '<=': return value <= threshold
  }
}

/** One group a run would find: its columns, its number (null for no data) and whether it fires. */
export interface GroupNow {
  group: Record<string, unknown>
  value: number | null
  firing: boolean
}

type Rule = Pick<MonitorSettings, 'notify_by_group' | 'alert_on_no_data'> & {
  operator: MonitorOperator | null
  threshold: number | null
}

/**
 * The groups a Threshold monitor's run over this table would find, as
 * MonitorEvaluation finds them: each row is a group keyed by its other
 * columns, or, when the Monitor doesn't notify by group, the worst row is its
 * one group. No rows fire the one group only with alert_on_no_data. Without
 * an operator and threshold nothing fires.
 */
export function groupsNow(table: LogTable, rule: Rule): { groups: GroupNow[]; error?: string } {
  const numbers = table.columns.flatMap((c, i) => (c.type === 'number' ? [i] : []))
  if (numbers.length !== 1) {
    return { groups: [], error: `The Query gives ${numbers.length} numbers per group, not one to compare with the threshold.` }
  }
  const at = numbers[0]
  const fires = (value: number | null) =>
    value === null || (rule.operator !== null && rule.threshold !== null && breached(value, rule.operator, rule.threshold))
  const worse = (a: number, b: number) => (rule.operator?.startsWith('<') ? Math.min(a, b) : Math.max(a, b))

  const groups = new Map<string, GroupNow>()
  for (const row of table.rows) {
    const value = row[at]
    if (typeof value !== 'number') continue
    const group = rule.notify_by_group
      ? Object.fromEntries(table.columns.flatMap((c, i) => (i === at ? [] : [[c.name, row[i]]])))
      : {}
    const key = JSON.stringify(group)
    const kept = groups.get(key)
    groups.set(key, { group, value: kept ? worse(kept.value as number, value) : value, firing: false })
  }
  if (groups.size === 0 && rule.alert_on_no_data) return { groups: [{ group: {}, value: null, firing: true }] }
  return { groups: [...groups.values()].map(g => ({ ...g, firing: fires(g.value) })) }
}

/**
 * The preview chart's Builder: the Query binned by time in bins as long as
 * the Monitor's range, so each bin is about what a run at its end would read.
 * Sort and take go, so every bin charts. Null without a function to chart.
 */
export function withRangeBins(builder: Builder, rangeMinutes: number): Builder | null {
  const summarize = builder.summarize
  if (!summarize || summarize.aggregates.length === 0) return null
  const { sort: _sort, take: _take, ...rest } = builder
  const by = [{ field: 'time', bin: `${rangeMinutes}m` }, ...summarize.by.filter(g => !isTimeBin(g))]
  return { ...rest, summarize: { ...summarize, by } }
}

/**
 * A Query from the explorer as a Threshold monitor can run it: its time bins
 * and any sort on them go, since a run compares one number per group over its
 * whole range.
 */
export function withoutTimeBins(builder: Builder): Builder {
  const summarize = builder.summarize
  if (!summarize || !summarize.by.some(isTimeBin)) return builder
  const next: Builder = { ...builder, summarize: { ...summarize, by: summarize.by.filter(g => !isTimeBin(g)) } }
  const sort = builder.sort?.filter(k => k.column !== 'time')
  if (sort && sort.length > 0) next.sort = sort
  else delete next.sort
  return next
}

function isTimeBin(g: { field: string; bin?: string }): boolean {
  return g.field === 'time' && g.bin !== undefined
}

/** A range as a Monitor's range: whole minutes, from 1 to MAX_RANGE_MINUTES. */
export function rangeMinutesOf(range: TimeRange): number {
  return Math.min(MAX_RANGE_MINUTES, Math.max(1, Math.round(rangeMs(range) / 60_000)))
}

/** The New page, starting from a Project, a Query and the range it ran over. */
export function newMonitorHref(orgSlug: string, projectId: number, query: string, range: TimeRange): string {
  const search = new URLSearchParams({ project_id: String(projectId), query, range_minutes: String(rangeMinutesOf(range)) })
  return `/${orgSlug}/monitors/new?${search}`
}

/** A group by its columns, such as `service: api`, or the Monitor's one group. */
export function groupLabel(group: Record<string, unknown>): string {
  const entries = Object.entries(group)
  if (entries.length === 0) return 'All Logs'
  return entries.map(([name, value]) => `${name}: ${typeof value === 'string' ? value : JSON.stringify(value)}`).join(', ')
}

/** What a run found, or why it failed, for the run history. */
export function runText(run: Pick<MonitorRun, 'status' | 'error' | 'firing' | 'matched' | 'logs_skipped'>, kind: MonitorKind): string {
  if (run.status === 'error') return run.error ?? 'The run failed.'
  let text: string
  if (kind === 'match') {
    const matched = run.matched ?? 0
    text = `${matched} ${matched === 1 ? 'Log' : 'Logs'} matched`
  } else {
    const firing = run.firing ?? 0
    text = firing === 0 ? 'Nothing firing' : `${firing} ${firing === 1 ? 'group' : 'groups'} firing`
  }
  return run.logs_skipped ? `${text}. Started again from the newest Logs, skipping those ingested since the last run.` : text
}
