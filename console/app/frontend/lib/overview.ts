// The Overview's props, as Console's Overview builder (app/services/overview.rb)
// answers them for the page and get_overview alike, and how the page writes
// its numbers.
import { tickLabel, valueTicks } from '@/lib/charts/axis'

import type { OverviewHealth } from './overviewHealth'
import type { OverviewOtherTools } from './overviewOtherTools'

export type OverviewRangeName = 'today' | '7d' | '30d'

export const OVERVIEW_RANGES: { value: OverviewRangeName; label: string }[] = [
  { value: 'today', label: 'Today' },
  { value: '7d', label: '7 days' },
  { value: '30d', label: '30 days' },
]

export interface OverviewRate {
  average: number
  peak: number
  /** The busiest second, ISO 8601; null when nothing arrived. */
  peak_at: string | null
}

export interface Overview {
  range: { name: OverviewRangeName; from: string; to: string; days: number; time_zone: 'UTC' }
  event_store: { available: true } | { available: false; error: string }
  /** logs is null when EventStore didn't answer. */
  empty: { projects: boolean; logs: boolean | null }
  ingest_counts: { since: string | null; starts_after_range: boolean } | null
  band: {
    received: { logs: number; bytes: number; bytes_on_wire: number; requests: number }
    /** What Log processing kept of the range's days: each Log's message and Attributes' JSON. */
    kept: { logs: number; bytes: number }
    stored: { logs: number; bytes: number; local_bytes: number; bucket_bytes: number }
    /** Kept bytes over stored bytes; null when nothing is stored. */
    ratio: number | null
    unreadable_logs: number
    dropped_by_transform: number
    overloaded_requests: number
  } | null
  rates: {
    seconds: number
    logs: OverviewRate
    bytes: OverviewRate
    requests: OverviewRate
    last_five_minutes: { logs_per_second: number } | null
  } | null
  daily: {
    series: OverviewSeries[]
    days: OverviewDay[]
  } | null
  projects: OverviewProject[] | null
  sources: { by_logs: OverviewSource[]; by_bytes: OverviewSource[] } | null
  health: OverviewHealth
  other_tools: OverviewOtherTools | null
}

/** A Service and file of a Project, over the range's days, by the Logs' own time. */
export interface OverviewSource {
  project: { id: number; name: string; slug: string; color: number }
  /** "" for none. */
  service: string
  /** "" for none. */
  file: string
  /** Logs kept. */
  logs: number
  /** Bytes kept. */
  bytes: number
  dropped_by_transform: number
  /** Logs kept and dropped by the Log transform. */
  sent: number
  /** Of Logs sent in by_logs, of bytes kept in by_bytes, 0 to 1; null when there were none. */
  share: number | null
}

/** A daily Chart's series: a Project, or Other for those past the seventh. */
export interface OverviewSeries {
  key: string
  name: string
  /** Its Chart colour's slot, 1 to 8, by creation order. */
  color: number
  project_ids: number[]
}

export interface OverviewDay {
  day: string
  /** Logs received, by series key. */
  logs: Record<string, number>
  received_bytes: number
  stored_bytes: number
  logs_per_second: OverviewRate
}

export interface OverviewProject {
  id: number
  name: string
  slug: string
  /** Its Chart colour's slot, Other's for a Project past the seventh. */
  color: number
  logs: number
  received_bytes: number
  kept_bytes: number
  stored_bytes: number
  /** Kept bytes over stored; null when nothing is stored. */
  ratio: number | null
  dropped_by_transform: number
  /** Its share of the bytes received, 0 to 1; null when none were. */
  share: number | null
  logs_per_second: OverviewRate
  retention_days: number
  retention_is_default: boolean
  oldest_day: string | null
  all_days: { logs: number; bytes: number }
  last_log_at: string | null
  /** Whether its last Log arrived over an hour ago; null when none has. */
  stale: boolean | null
  has_logs: boolean
}

/** A Chart colour's slot as a colour. */
export function chartColor(slot: number): string {
  return `var(--chart-${slot})`
}

/** A UTC day as a Chart's axis names it: Oct 2. */
export function dayText(day: string): string {
  return new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', timeZone: 'UTC' }).format(new Date(`${day}T00:00:00Z`))
}

/** A share as a percentage: 42%, 0.4%, under 0.1%. */
export function shareText(share: number): string {
  if (share > 0 && share < 0.001) return 'under 0.1%'
  return `${new Intl.NumberFormat('en', { maximumSignificantDigits: share < 0.1 ? 2 : 3 }).format(share * 100)}%`
}

const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']

/** Bytes in binary units, three significant digits: 1.21 GiB. */
export function bytesText(bytes: number): string {
  let value = bytes
  let unit = 0
  while (Math.abs(value) >= 1024 && unit < UNITS.length - 1) {
    value /= 1024
    unit++
  }
  const digits = unit === 0 ? { maximumFractionDigits: 0 } : { maximumSignificantDigits: 3 }
  return `${new Intl.NumberFormat('en', digits).format(value)} ${UNITS[unit]}`
}

/**
 * A bytes axis up to max: round steps of the binary unit max reads in, so
 * ticks read 0, 256 KiB, 512 KiB rather than 195 KiB.
 */
export function bytesTicks(max: number): { domain: [number, number]; ticks: number[]; label: (bytes: number) => string } {
  let unit = 0
  while (max >= 1024 ** (unit + 1) && unit < UNITS.length - 1) unit++
  const size = 1024 ** unit
  const { domain, ticks, step } = valueTicks(0, max / size, 4)
  const tick = tickLabel(step)
  return {
    domain: [domain[0] * size, domain[1] * size],
    ticks: ticks.map(t => t * size),
    label: bytes => (bytes === 0 ? '0' : `${tick(bytes / size)} ${UNITS[unit]}`),
  }
}

/** A count, abbreviated from a thousand up (12.3K, 4.56M); exactText gives the whole number. */
export function countText(count: number): string {
  return Math.abs(count) >= 1000
    ? new Intl.NumberFormat('en', { notation: 'compact', maximumSignificantDigits: 3 }).format(count)
    : exactText(count)
}

/** A count in full, for a tooltip: 12,345. */
export function exactText(count: number): string {
  return new Intl.NumberFormat('en', { maximumFractionDigits: 2 }).format(count)
}

/** A rate a second: three significant digits under 1000, abbreviated above. */
export function rateText(rate: number): string {
  if (rate === 0) return '0'
  if (Math.abs(rate) >= 1000) return countText(rate)
  return new Intl.NumberFormat('en', { maximumSignificantDigits: 3 }).format(rate)
}

/** A second as the page names it, in UTC: Oct 2, 12:00:05 UTC. */
export function secondText(iso: string): string {
  const at = new Date(iso)
  const day = new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', timeZone: 'UTC' }).format(at)
  const time = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit', second: '2-digit', timeZone: 'UTC' }).format(at)
  return `${day}, ${time} UTC`
}
