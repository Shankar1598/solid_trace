// The Overview's props, as Console's Overview builder (app/services/overview.rb)
// answers them for the page and get_overview alike, and how the page writes
// its numbers.

import type { OverviewHealth } from './overviewHealth'

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
    stored: { logs: number; bytes: number; local_bytes: number; bucket_bytes: number }
    /** Received bytes over stored bytes; null when nothing is stored. */
    ratio: number | null
    unreadable_logs: number
    overloaded_requests: number
  } | null
  rates: {
    seconds: number
    logs: OverviewRate
    bytes: OverviewRate
    requests: OverviewRate
    last_five_minutes: { logs_per_second: number } | null
  } | null
  health: OverviewHealth
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
