import type { ReactNode } from 'react'
import { Link } from '@inertiajs/react'
import { AlertTriangle, CheckCircle2, XCircle } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { countText, secondText } from '@/lib/overview'
import { msText, waitText, type HealthStatus, type OverviewHealth as Health } from '@/lib/overviewHealth'

const TONES = { ok: 'ok', warning: 'warn', error: 'error' } as const
const ICONS = { ok: CheckCircle2, warning: AlertTriangle, error: XCircle }

/** A status as a colour, an icon and words, never colour alone. */
function StatusBadge({ status, children }: { status: HealthStatus; children: ReactNode }) {
  const Icon = ICONS[status]
  return (
    <Badge variant={TONES[status]} data-health-status={status}>
      <Icon aria-hidden />
      {children}
    </Badge>
  )
}

const WAIT_WORDS = { ok: 'Keeping up', warning: 'Behind', error: 'Far behind' }
const READ_WORDS = { ok: 'Fast', warning: 'Slow', error: 'Very slow' }

/**
 * The Overview's Health: Log processing's waits and Log reads' times per
 * Project, the slowest reads, failing and Firing Monitors, and the range's
 * new Issues and Events. What EventStore answers is null when it didn't;
 * Monitors and Issues are Console's and always show.
 */
export function OverviewHealth({ health, orgSlug }: { health: Health; orgSlug: string }) {
  const { projects, slowest_reads: slowest, monitors, issues, events } = health
  return (
    <div className="space-y-8">
      {projects && (
        <div className="space-y-2" data-health="projects">
          <h3 className="text-sm font-medium">Log processing and reads</h3>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Project</TableHead>
                <TableHead>Oldest Log batch waiting</TableHead>
                <TableHead>Reads, last 24 hours</TableHead>
                <TableHead className="text-right">Median</TableHead>
                <TableHead className="text-right">95th percentile</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {projects.map(({ project, processing, reads }) => (
                <TableRow key={project.id} data-health-project={project.slug}>
                  <TableCell className="font-medium">{project.name}</TableCell>
                  <TableCell>
                    <span className="inline-flex items-center gap-2">
                      <StatusBadge status={processing.status}>{WAIT_WORDS[processing.status]}</StatusBadge>
                      <span className="tabular-nums text-muted-foreground">
                        {processing.oldest_batch_at ? waitText(processing.waited_seconds) : 'Nothing waiting'}
                      </span>
                    </span>
                  </TableCell>
                  <TableCell>
                    {reads ? (
                      <span className="inline-flex items-center gap-2">
                        <StatusBadge status={reads.status}>{READ_WORDS[reads.status]}</StatusBadge>
                        <span className="tabular-nums text-muted-foreground">{countText(reads.reads)}</span>
                      </span>
                    ) : <span className="text-muted-foreground">No reads</span>}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{reads ? msText(reads.median_ms) : '–'}</TableCell>
                  <TableCell className="text-right tabular-nums">{reads ? msText(reads.p95_ms) : '–'}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {slowest && slowest.length > 0 && (
        <div className="space-y-2" data-health="slowest-reads">
          <h3 className="text-sm font-medium">Slowest reads, last 24 hours</h3>
          <ul className="space-y-1 text-sm">
            {slowest.map((read, i) => (
              <li key={i} className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <StatusBadge status={read.status}>{msText(read.duration_ms)}</StatusBadge>
                <span>{capitalised(read.kind)}</span>
                <span className="text-muted-foreground">in {read.project.name}, {secondText(read.at)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="space-y-2" data-health="monitors">
        <h3 className="text-sm font-medium">Monitors</h3>
        {monitors.firing.length === 0 && monitors.failing.length === 0 ? (
          <p className="text-sm"><StatusBadge status="ok">OK</StatusBadge> <span className="text-muted-foreground">No Monitor is Firing or failing to run.</span></p>
        ) : (
          <ul className="space-y-1 text-sm">
            {monitors.firing.map((monitor) => (
              <li key={`firing-${monitor.id}`} className="flex flex-wrap items-center gap-x-2 gap-y-1" data-health-monitor="firing">
                <StatusBadge status={monitor.status}>{monitor.firing_count > 1 ? `Firing (${monitor.firing_count})` : 'Firing'}</StatusBadge>
                <Link href={monitor.path} className="font-medium underline-offset-2 hover:underline">{monitor.name}</Link>
                <span className="text-muted-foreground">in {monitor.project.name}, since {secondText(monitor.since)}</span>
              </li>
            ))}
            {monitors.failing.map((monitor) => (
              <li key={`failing-${monitor.id}`} className="flex flex-wrap items-center gap-x-2 gap-y-1" data-health-monitor="failing">
                <StatusBadge status={monitor.status}>Last run failed</StatusBadge>
                <Link href={monitor.path} className="font-medium underline-offset-2 hover:underline">{monitor.name}</Link>
                <span className="text-muted-foreground" title={monitor.last_error}>in {monitor.project.name}: {monitor.last_error}</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="space-y-2" data-health="issues">
        <h3 className="text-sm font-medium">Issues and Events</h3>
        <p className="flex flex-wrap items-center gap-2 text-sm">
          <StatusBadge status={issues.status}>{issues.new === 0 ? 'No new Issues' : `${countText(issues.new)} new ${issues.new === 1 ? 'Issue' : 'Issues'}`}</StatusBadge>
          <span className="text-muted-foreground">
            {events ? `${countText(events.new)} ${events.new === 1 ? 'Event' : 'Events'} in the range.` : 'Events are unavailable.'}
          </span>
          {issues.new > 0 && (
            <Link href={`/${orgSlug}/issues`} className="underline underline-offset-2">All Issues</Link>
          )}
        </p>
        {issues.newest.length > 0 && (
          <ul className="space-y-1 text-sm">
            {issues.newest.map((issue) => (
              <li key={issue.path} className="flex flex-wrap gap-x-2">
                <Link href={issue.path} className="font-medium underline-offset-2 hover:underline">{issue.title}</Link>
                <span className="text-muted-foreground">in {issue.project.name}, first seen {secondText(issue.first_seen_at)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function capitalised(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1)
}
