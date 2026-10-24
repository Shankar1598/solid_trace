// The Overview's Health section, as Overview::Health
// (app/services/overview/health.rb) answers it, and how the page writes its
// times.

export type HealthStatus = 'ok' | 'warning' | 'error'

export interface HealthProject {
  id: number
  name: string
  slug: string
}

export interface OverviewHealth {
  /** Each Project in the Organization's order; null when EventStore didn't answer. */
  projects: {
    project: HealthProject
    processing: { oldest_batch_at: string | null; waited_seconds: number; status: HealthStatus }
    /** The last 24 hours' reads; null for none. */
    reads: { reads: number; median_ms: number; p95_ms: number; status: HealthStatus } | null
    events: number
  }[] | null
  slowest_reads: { project: HealthProject; kind: string; at: string; duration_ms: number; status: HealthStatus }[] | null
  monitors: {
    failing: { id: number; name: string; project: HealthProject; path: string; last_error: string; last_run_at: string | null; status: HealthStatus }[]
    firing: { id: number; name: string; project: HealthProject; path: string; firing_count: number; since: string; status: HealthStatus }[]
  }
  issues: {
    new: number
    newest: { number: number; title: string; project: HealthProject; first_seen_at: string; path: string }[]
    status: HealthStatus
  }
  /** null when EventStore didn't answer. */
  events: { new: number } | null
}

/** A read's time: 840 ms, 2.5 s. */
export function msText(ms: number): string {
  if (ms < 1000) return `${new Intl.NumberFormat('en', { maximumSignificantDigits: 3 }).format(ms)} ms`
  return `${new Intl.NumberFormat('en', { maximumFractionDigits: 1 }).format(ms / 1000)} s`
}

/** A wait: 45 s, 2 min 5 s, 3 h 10 min. */
export function waitText(seconds: number): string {
  const s = Math.round(seconds)
  if (s < 60) return `${s} s`
  if (s < 3600) return `${Math.floor(s / 60)} min${s % 60 ? ` ${s % 60} s` : ''}`
  const minutes = Math.floor((s % 3600) / 60)
  return `${Math.floor(s / 3600)} h${minutes ? ` ${minutes} min` : ''}`
}
