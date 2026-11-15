/**
 * The Dispatcher's status as Settings → Instance loads it after the page
 * shows (InstanceSettingsController#dispatcher_status): what the Dispatcher
 * answered, or Console's error when it did not answer as it should.
 */
export type DispatcherStatus =
  | { version?: string; token_active: boolean; token_error?: string; error?: undefined }
  | { error: string }

/** The line that the page shows for a status, and whether all is well. */
export function dispatcherStatusLine(status: DispatcherStatus): { ok: boolean; text: string } {
  if ('error' in status && status.error) return { ok: false, text: status.error }

  const { version, token_active: tokenActive, token_error: tokenError } = status as Exclude<DispatcherStatus, { error: string }>
  const answers = `The Dispatcher answers (${version || 'a development build'})`
  if (tokenActive) return { ok: true, text: `${answers}, and SolidTrace accepts its token.` }

  return {
    ok: false,
    text: tokenError
      ? `${answers}, but SolidTrace refuses its token: ${tokenError}`
      : `${answers}, but SolidTrace refuses its token. An Admin makes a new Dispatcher's token in Settings → API tokens & MCP.`,
  }
}

/** A Project's Factory settings, as the Dispatcher keeps them. */
export interface FactorySettingsValues {
  repository: string
  folder: string
  base_branch: string
}

/**
 * A tracker route: the ready tickets of the tracker connection whose
 * routing field has the value `match` go to the Project. An empty `match`
 * is the default of the connection.
 */
export interface FactoryRoute {
  connection: string
  match: string
}

/** A schedule of the Project, as the Dispatcher keeps it. */
export interface FactorySchedule {
  id: string
  name: string
  cron: string
  time_zone: string
  base_branch: string
  paused: boolean
  /** The next fire time (null while the schedule is paused). */
  next_at: string | null
  /** The last Run that the schedule started, with its Run page. */
  last_run: { id: string; state: string; fired_at: string; path: string } | null
  pause_path: string
  continue_path: string
}

/**
 * The Factory section's data as a Project's page loads it after it shows
 * (FactorySettings#section): the settings (null when the Project has none),
 * the tracker routes and the schedules, and for an Admin the tracker
 * connections for a new route. Or Console's error when the Dispatcher did
 * not answer as it should.
 */
export type FactorySectionData =
  | {
      settings: FactorySettingsValues | null
      routes: FactoryRoute[]
      schedules: FactorySchedule[]
      tracker_connections?: string[]
      error?: undefined
    }
  | { error: string }

/** A route's match value as the section shows it. */
export function routeMatchText(match: string): string {
  return match === '' ? 'Each other ticket (the default)' : `“${match}”`
}

/**
 * A schedule's next fire time in the schedule's own time zone (UTC when
 * this browser does not know the zone), or why it has none.
 */
export function scheduleNextText(schedule: Pick<FactorySchedule, 'paused' | 'next_at' | 'time_zone'>): string {
  if (schedule.paused) return 'Paused'
  if (!schedule.next_at) return 'None'

  const options: Intl.DateTimeFormatOptions = {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZoneName: 'short',
  }
  const at = new Date(schedule.next_at)
  try {
    return new Intl.DateTimeFormat('en-US', { ...options, timeZone: schedule.time_zone || 'UTC' }).format(at)
  } catch {
    return new Intl.DateTimeFormat('en-US', { ...options, timeZone: 'UTC' }).format(at)
  }
}

/**
 * The Factory settings that the section shows. After a save, the redirect
 * back to the Project's page has no `factory` until the page loads it again.
 * The section keeps the last settings in that time, so the form stays
 * mounted: it keeps the values that the Admin typed and the error that the
 * Dispatcher gave.
 */
export function factorySectionShown(
  current: FactorySectionData | undefined,
  last: FactorySectionData | undefined,
): FactorySectionData | undefined {
  return current ?? last
}

/**
 * Where the Factory section saves, its link to the Project's Runs, and
 * where an Admin adds (POST) and removes (DELETE) a tracker route.
 */
export interface FactorySectionPaths {
  path: string
  runs_path: string
  routes_path: string
}
