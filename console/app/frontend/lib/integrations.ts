const PROVIDER_NAMES: Record<string, string> = { slack: 'Slack', pagerduty: 'PagerDuty', email: 'Email' }

/** A provider as people write it: PagerDuty, not pagerduty or Pagerduty. */
export function providerName(provider: string): string {
  return PROVIDER_NAMES[provider] ?? provider
}
