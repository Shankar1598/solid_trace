import { Link, usePage } from '@inertiajs/react'
import { formatDistanceToNow } from 'date-fns'
import { BellRing, Plus } from 'lucide-react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { buttonVariants } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import type { Monitor } from '@/lib/monitors/monitor'
import type { SharedProps } from '@/types'
import { MonitorStatusBadge } from './components/MonitorStatusBadge'

/** The Organization's Monitors: each one's status, Project, kind and last run. */
export default function MonitorsIndex({ monitors }: { monitors: Monitor[] }) {
  const orgSlug = usePage<SharedProps>().props.current_org?.slug ?? ''

  return (
    <DashboardLayout reserveBreadcrumbSpace>
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <h1 className="text-3xl font-bold tracking-tight">Monitors</h1>
          <Link href={`/${orgSlug}/monitors/new`} className={buttonVariants()}>
            <Plus className="mr-2 h-4 w-4" />
            New monitor
          </Link>
        </div>

        {monitors.length === 0 ? (
          <Card className="border-dashed">
            <CardContent className="flex flex-col items-center justify-center py-12 text-center">
              <BellRing className="mb-4 h-12 w-12 text-muted-foreground opacity-50" />
              <h3 className="text-lg font-medium">No monitors yet</h3>
              <p className="mb-4 max-w-md text-sm text-muted-foreground">
                A Monitor runs a Query every few minutes and tells your Integrations when a number passes its threshold, or when new Logs match.
                Make one here, or chart a Query in the Logs explorer and press Create monitor beside the chart switch.
              </p>
              <Link href={`/${orgSlug}/monitors/new`} className={buttonVariants()}>New monitor</Link>
            </CardContent>
          </Card>
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full text-sm">
              <thead className="bg-muted text-left text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2 font-medium">Name</th>
                  <th className="px-4 py-2 font-medium">Status</th>
                  <th className="px-4 py-2 font-medium">Project</th>
                  <th className="px-4 py-2 font-medium">Kind</th>
                  <th className="px-4 py-2 font-medium">Last run</th>
                </tr>
              </thead>
              <tbody>
                {monitors.map(m => (
                  <tr key={m.id} className="border-t hover:bg-muted/50" data-monitor-row={m.name}>
                    <td className="px-4 py-2">
                      <Link href={`/${orgSlug}/monitors/${m.id}`} className="font-medium hover:underline">{m.name}</Link>
                    </td>
                    <td className="px-4 py-2">
                      <MonitorStatusBadge monitor={m} />
                    </td>
                    <td className="px-4 py-2">{m.project.name}</td>
                    <td className="px-4 py-2">{m.kind === 'threshold' ? 'Threshold' : 'Match'}</td>
                    <td className="px-4 py-2 text-muted-foreground">
                      {m.last_run_at ? `${formatDistanceToNow(new Date(m.last_run_at))} ago` : 'Not run yet'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </DashboardLayout>
  )
}
