import { Link, usePage } from '@inertiajs/react'
import { format } from 'date-fns'
import { AlertTriangle, ClipboardCheck } from 'lucide-react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { buttonVariants } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { agentSessionPath } from '@/lib/agentSessions'
import { prHref, recordPath, type ListedReviewRecord } from '@/lib/reviewRecords'
import type { SharedProps } from '@/types'

type ReviewRecordsProps =
  | { on: false; can_turn_on: boolean; settings_path: string }
  | { on: true; settings_path: string; event_store: { available: boolean; error?: string }; rows: ListedReviewRecord[]; more: boolean }

const DESCRIPTION = 'The newest Review record of each PR: what the reviews found, what happened to each finding, and what they missed.'

/**
 * The Review records page of Coding agents: one row for each PR whose
 * open-pr or update-pr session uploaded with its Agent session, the PR
 * stored last first.
 */
export default function ReviewRecordsIndex({ review_records: view }: { review_records: ReviewRecordsProps }) {
  return (
    <DashboardLayout>
      <PageHeader title="Review records" description={DESCRIPTION} />
      {view.on ? <RecordsList view={view} /> : <TurnOn canTurnOn={view.can_turn_on} settingsPath={view.settings_path} />}
    </DashboardLayout>
  )
}

function TurnOn({ canTurnOn, settingsPath }: { canTurnOn: boolean; settingsPath: string }) {
  return (
    <Card className="border-dashed" data-notice="agent-sessions-off">
      <CardContent className="flex flex-col items-center justify-center py-12 text-center">
        <ClipboardCheck className="mb-4 h-12 w-12 text-muted-foreground opacity-50" aria-hidden />
        <h2 className="text-lg font-medium">Agent sessions are off</h2>
        <p className="mb-4 max-w-md text-sm text-muted-foreground">
          Review records come with the Agent sessions that upload.{' '}
          {canTurnOn
            ? 'Turn them on in Settings → Agent sessions, with the repositories whose Agent sessions upload.'
            : 'An Admin of this Organization turns them on in Settings → Agent sessions.'}
        </p>
        <Link href={settingsPath} className={buttonVariants({ variant: canTurnOn ? 'default' : 'outline' })}>
          {canTurnOn ? 'Turn on Agent sessions' : 'Open the settings'}
        </Link>
      </CardContent>
    </Card>
  )
}

function RecordsList({ view }: { view: Extract<ReviewRecordsProps, { on: true }> }) {
  const { current_org } = usePage<SharedProps>().props
  const slug = current_org?.slug ?? ''
  const path = `/${slug}/review_records`
  if (!view.event_store.available) {
    return (
      <div role="alert" className="flex items-start gap-2 border border-warning/40 px-3 py-2 text-sm">
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
        <span>{view.event_store.error}</span>
      </div>
    )
  }
  if (view.rows.length === 0) {
    return (
      <Card className="border-dashed" data-notice="no-review-records">
        <CardContent className="flex flex-col items-center justify-center py-12 text-center">
          <ClipboardCheck className="mb-4 h-12 w-12 text-muted-foreground opacity-50" aria-hidden />
          <h2 className="text-lg font-medium">No Review records</h2>
          <p className="max-w-md text-sm text-muted-foreground">
            The open-pr and update-pr skills of the solidtrace plugin end by writing the PR's Review record with solidtrace review final. It shows here when the Agent session uploads.
          </p>
        </CardContent>
      </Card>
    )
  }
  return (
    <div className="space-y-2">
      <div className="overflow-x-auto border">
        <Table aria-label="Review records">
          <TableHeader>
            <TableRow>
              <TableHead>PR</TableHead>
              <TableHead className="text-right">Round</TableHead>
              <TableHead className="max-md:hidden">Stored</TableHead>
              <TableHead className="text-right">Findings</TableHead>
              <TableHead className="text-right">Fixed</TableHead>
              <TableHead className="text-right">Missed</TableHead>
              <TableHead className="text-right max-md:hidden">Confirmations not given</TableHead>
              <TableHead className="max-md:hidden">Agent session</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {view.rows.map(row => (
              <TableRow key={row.key} data-review-record={row.key}>
                <TableCell className="max-w-56 md:max-w-md" data-cell="pr">
                  <Link href={recordPath(path, { key: row.key })} title={row.pr || row.key} className="block truncate font-medium underline-offset-2 hover:underline">
                    {row.pr || <span className="font-normal text-muted-foreground">No PR</span>}
                  </Link>
                  {row.title && <div className="truncate text-xs text-muted-foreground" title={row.title}>{row.title}</div>}
                  {prHref(row.pr) && (
                    <a href={prHref(row.pr)!} target="_blank" rel="noreferrer noopener" className="text-xs text-muted-foreground underline underline-offset-2">
                      Open the PR
                    </a>
                  )}
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums" data-cell="round">
                  {row.round}
                  {row.earlier > 0 && <span className="ml-1 font-sans text-muted-foreground" title={`${row.earlier} earlier`}>({row.earlier} earlier)</span>}
                </TableCell>
                <TableCell className="font-mono text-xs whitespace-nowrap max-md:hidden">
                  <time dateTime={row.stored_at}>{format(new Date(row.stored_at), 'MMM d, HH:mm')}</time>
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums" data-cell="findings">{row.findings}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums" data-cell="fixed">{row.fixed}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums" data-cell="missed">{row.missed}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums max-md:hidden" data-cell="not-given">{row.confirmations_not_given}</TableCell>
                <TableCell className="max-md:hidden">
                  <Link href={agentSessionPath(slug, row.session_id)} className="font-mono text-xs underline underline-offset-2">
                    {row.session_id.slice(0, 8)}
                  </Link>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      {view.more && <p className="text-xs text-muted-foreground">The {view.rows.length.toLocaleString()} PRs stored last.</p>}
    </div>
  )
}
