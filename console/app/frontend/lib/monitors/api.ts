// Calls to Console the Monitor form makes with fetch rather than an Inertia
// visit: the groups a Monitor comparing the change would fire now.
import { csrfToken } from '@/lib/apiTokens'
import { RunError, runFailure } from '@/lib/logs/api'
import type { MonitorSettings } from './monitor'

/** One group a run now would find: its columns, number, Baseline and whether it would fire. */
export interface PreviewGroup {
  group: Record<string, unknown>
  /** Null for no data. */
  value: number | null
  /** Its number over the Baseline range, 0 when it had no row there; null for no data. */
  baseline: number | null
  firing: boolean
}

/** What a run now would read and find: the current range, the Baseline's, and each group. */
export interface GroupsPreview {
  from: string
  to: string
  baseline_from: string | null
  baseline_to: string | null
  groups: PreviewGroup[]
}

export type PreviewSettings = Pick<MonitorSettings,
  'project_id' | 'query' | 'compare' | 'baseline' | 'operator' | 'notify_by_group' | 'alert_on_no_data'> & {
  range_minutes: number
  threshold: number | null
}

/**
 * The groups a run of a Monitor with these settings, not yet saved, would
 * find now: read over both ranges and compared on the server, as a run reads
 * and compares them, so the form shows the run's own rule. A failed, timed
 * out or cut read throws a RunError saying why.
 */
export async function fetchGroupsPreview(orgSlug: string, settings: PreviewSettings, signal?: AbortSignal): Promise<GroupsPreview> {
  let response: Response
  try {
    response = await fetch(`/${orgSlug}/monitors/preview_groups`, {
      method: 'POST',
      signal,
      credentials: 'same-origin',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken() },
      body: JSON.stringify({ monitor: settings }),
    })
  } catch (error) {
    if (signal?.aborted) throw error
    throw new RunError('Console could not be reached. Check your connection and try again.')
  }
  const body = await response.json().catch(() => null)
  if (response.ok && Array.isArray(body?.groups)) return body as GroupsPreview
  throw runFailure(body, response.status)
}
