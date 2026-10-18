import { Link, usePage } from '@inertiajs/react'
import { formatDistanceToNow } from 'date-fns'
import { BellRing, Plus } from 'lucide-react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { buttonVariants } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { Monitor } from '@/lib/monitors/monitor'
import type { SharedProps } from '@/types'
import { MonitorStatusBadge } from './components/MonitorStatusBadge'

/** The Organization's Monitors: each one's status, Project, kind and last run. */
export default function MonitorsIndex({ monitors }: { monitors: Monitor[] }) {
  const orgSlug = usePage<SharedProps>().props.current_org?.slug ?? ''

  return (
    <DashboardLayout>
      <PageHeader
        title="Monitors"
        actions={
          <Link href={`/${orgSlug}/monitors/new`} className={buttonVariants()}>
            <Plus className="h-4 w-4" />
            New monitor
          </Link>
        }
      />

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
        <div className="border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Project</TableHead>
                <TableHead>Kind</TableHead>
                <TableHead>Last run</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {monitors.map(m => (
                <TableRow key={m.id} data-monitor-row={m.name}>
                  <TableCell>
                    <Link href={`/${orgSlug}/monitors/${m.id}`} className="font-medium hover:underline">{m.name}</Link>
                  </TableCell>
                  <TableCell>
                    <MonitorStatusBadge monitor={m} />
                  </TableCell>
                  <TableCell>{m.project.name}</TableCell>
                  <TableCell>{m.kind === 'threshold' ? 'Threshold' : 'Match'}</TableCell>
                  <TableCell className="text-muted-foreground">
                    {m.last_run_at ? `${formatDistanceToNow(new Date(m.last_run_at))} ago` : 'Not run yet'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </DashboardLayout>
  )
}
