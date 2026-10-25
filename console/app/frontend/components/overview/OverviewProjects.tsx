import { Link, router } from '@inertiajs/react'
import { formatDistanceToNow } from 'date-fns'
import { Clock } from 'lucide-react'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { statText, valueText } from '@/lib/charts/axis'
import { bytesText, chartColor, dayText, rateText, secondText, shareText, type OverviewProject } from '@/lib/overview'

/**
 * The Overview's Projects, one row each in creation order, wearing the
 * colour each wears in the Logs per day Chart. A row opens that Project's
 * Logs; a Project with no Logs yet says so and links to its setup, which is
 * its Logs page's Get started.
 */
export function OverviewProjects({ projects, orgSlug }: { projects: OverviewProject[]; orgSlug: string }) {
  const logsPath = (p: OverviewProject) => `/${orgSlug}/projects/${p.slug}/logs`
  return (
    <div>
      <Table aria-label="Projects">
        <TableHeader>
          <TableRow>
            <TableHead>Project</TableHead>
            <TableHead className="text-right">Logs</TableHead>
            <TableHead className="text-right">Received</TableHead>
            <TableHead className="text-right" title="Each Log's message and Attributes, after the Log transform">Kept</TableHead>
            <TableHead className="text-right">Stored</TableHead>
            <TableHead className="text-right" title="Kept bytes over stored bytes">Ratio</TableHead>
            <TableHead className="text-right" title="Of the bytes received">Share</TableHead>
            <TableHead className="text-right">Logs per second</TableHead>
            <TableHead className="text-right">Retention</TableHead>
            <TableHead className="text-right" title="Every day kept, the range's and older">Kept in all</TableHead>
            <TableHead>Last Log</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {projects.map(p => (
            <TableRow
              key={p.id}
              data-project={p.slug}
              className="cursor-pointer"
              onClick={event => {
                if ((event.target as HTMLElement).closest('a')) return
                router.visit(logsPath(p))
              }}
            >
              <TableCell>
                <span className="flex items-center gap-2">
                  <span className="h-2 w-2 shrink-0" style={{ background: chartColor(p.color) }} aria-hidden />
                  <Link href={logsPath(p)} className="font-medium hover:underline">{p.name}</Link>
                </span>
              </TableCell>
              {p.has_logs ? (
                <>
                  <Num value={statText(p.logs)} exact={valueText(p.logs)} />
                  <Num value={bytesText(p.received_bytes)} />
                  <Num value={bytesText(p.kept_bytes)} />
                  <Num value={bytesText(p.stored_bytes)} />
                  <Num value={p.ratio === null ? '—' : `${p.ratio}×`} />
                  <Num value={p.share === null ? '—' : shareText(p.share)} />
                  <TableCell className="text-right font-mono tabular-nums whitespace-nowrap">
                    <span title={valueText(p.logs_per_second.average)}>{rateText(p.logs_per_second.average)}</span>
                    <span className="text-muted-foreground"> avg · </span>
                    <span title={p.logs_per_second.peak_at ? `at ${secondText(p.logs_per_second.peak_at)}` : undefined}>{rateText(p.logs_per_second.peak)}</span>
                    <span className="text-muted-foreground"> peak</span>
                  </TableCell>
                  <TableCell className="text-right font-mono tabular-nums whitespace-nowrap">
                    <div>{p.retention_days} days{p.retention_is_default && <span className="text-muted-foreground"> (default)</span>}</div>
                    {p.oldest_day && <div className="text-xs text-muted-foreground">since {dayText(p.oldest_day)}</div>}
                  </TableCell>
                  <TableCell className="text-right font-mono tabular-nums whitespace-nowrap">
                    <div title={valueText(p.all_days.logs)}>{statText(p.all_days.logs)} Logs</div>
                    <div className="text-xs text-muted-foreground">{bytesText(p.all_days.bytes)}</div>
                  </TableCell>
                  <TableCell className="whitespace-nowrap"><LastLog project={p} /></TableCell>
                </>
              ) : (
                <TableCell colSpan={10} className="text-muted-foreground" data-empty>
                  No Logs yet.{' '}
                  <Link href={logsPath(p)} className="font-medium text-foreground underline underline-offset-2">See how to send them</Link>
                </TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function Num({ value, exact }: { value: string; exact?: string }) {
  return <TableCell className="text-right font-mono tabular-nums whitespace-nowrap" title={exact}>{value}</TableCell>
}

/** When the last Log arrived, marked with an icon and words when over an hour ago. */
function LastLog({ project }: { project: OverviewProject }) {
  if (!project.last_log_at) return <span className="text-muted-foreground">Not counted yet</span>
  const ago = `${formatDistanceToNow(new Date(project.last_log_at))} ago`
  return project.stale ? (
    <span className="inline-flex items-center gap-1" title={secondText(project.last_log_at)} data-stale>
      <Clock className="h-3.5 w-3.5 text-warning" aria-hidden />
      {ago}
      <span className="text-xs text-muted-foreground">· over an hour</span>
    </span>
  ) : (
    <span title={secondText(project.last_log_at)}>{ago}</span>
  )
}
