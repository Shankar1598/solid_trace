import { Link, usePage } from '@inertiajs/react'
import { BellRing } from 'lucide-react'
import { newMonitorHref } from '@/lib/monitors/monitor'
import type { TimeRange } from '@/lib/timeRange'
import type { SharedProps } from '@/types'

/**
 * Create monitor, beside Add to dashboard: opens the New monitor page with
 * the Project, the Query that ran and the range it ran over.
 */
export function CreateMonitor({ projectId, query, range }: { projectId: number; query: string; range: TimeRange }) {
  const orgSlug = usePage<SharedProps>().props.current_org?.slug ?? ''
  return (
    <Link
      href={newMonitorHref(orgSlug, projectId, query, range)}
      className="flex h-6 shrink-0 items-center gap-1 border px-2 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
    >
      <BellRing className="h-3 w-3" /> Create monitor
    </Link>
  )
}
