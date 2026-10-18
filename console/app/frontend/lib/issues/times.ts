import { formatDistance } from 'date-fns'
import type { Issue } from '@/types'

// How long ago the Issue's first Event happened, as the Issues page's Age
// column shows it. first_seen_at is the Issue's creation until an Event is
// counted.
export function issueAge(issue: Pick<Issue, 'first_seen_at'>, now = new Date()): string {
  return formatDistance(new Date(issue.first_seen_at), now).replace('about ', '')
}
