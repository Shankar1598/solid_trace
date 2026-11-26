import type { IntegrationOption, Monitor } from './monitor'

/** Since when a Monitor that is on does not run, for the Monitors list. null while it runs, and while it is off. */
export function notRunningSince(monitor: Pick<Monitor, 'enabled' | 'not_running_since'>): string | null {
  return monitor.enabled ? monitor.not_running_since : null
}

/** The select's value for no Monitor status Integration. */
export const NO_MONITOR_STATUS = 'none'

const PROVIDER_NAMES: Record<string, string> = { slack: 'Slack', pagerduty: 'PagerDuty', email: 'Email' }

/** The choices of the Monitor status select: None, then each Integration with its provider, such as `Ops (Slack)`. */
export function monitorStatusItems(options: IntegrationOption[]): { value: string; label: string }[] {
  return [
    { value: NO_MONITOR_STATUS, label: 'None' },
    ...options.map(option => ({
      value: String(option.id),
      label: `${option.name} (${PROVIDER_NAMES[option.provider] ?? option.provider})${option.enabled ? '' : ', off'}`,
    })),
  ]
}

/** The Integration id that the form sends for a select value: empty for None. */
export function monitorStatusParam(value: string | null): string {
  return !value || value === NO_MONITOR_STATUS ? '' : value
}
