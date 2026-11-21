import { Link, router } from '@inertiajs/react'
import { format } from 'date-fns'
import { AlertTriangle, Square } from 'lucide-react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import {
  actorText,
  checkText,
  durationText,
  eventKindText,
  stateText,
  stateTone,
  type RunEvent,
  type RunExecution,
  type RunReport,
  type RunRow,
  type RunTakeOver,
} from '@/lib/runs'

interface RunProps {
  event_store: { available: boolean; error?: string }
  run: RunRow
  project: { name: string; slug: string } | null
  executions: RunExecution[]
  events: RunEvent[]
  interventions: RunEvent[]
  report: RunReport | null
  agent_sessions: { id: string; path: string }[]
  take_over: RunTakeOver | null
  commands: { message: string; take_over: string; stop: string }
  list_path: string
  stop: { can: boolean; path: string; sent_state: string | null }
}

function when(time: string | null): string {
  return time ? format(new Date(time), 'MMM d, HH:mm:ss') : '-'
}

function None({ text = 'None' }: { text?: string }) {
  return <span className="text-muted-foreground">{text}</span>
}

/**
 * The Run page: one Run from its run events, with its Executions, events
 * and interventions, the last report with its checks and Evidence, its
 * Agent sessions, the CLI commands and the Stop button.
 */
export default function RunShow({ run: view }: { run: RunProps }) {
  const run = view.run
  return (
    <DashboardLayout>
      <PageHeader
        breadcrumbs={[{ label: 'Runs', href: view.list_path }, { label: run.title || run.id }]}
        title={run.title || 'Run'}
        tabTitle="Run"
        badge={
          <span className="flex items-center gap-1.5">
            <Badge variant={stateTone(run.state)}>{stateText(run.state)}</Badge>
            {run.active && <Badge variant="count">Active</Badge>}
            {run.closed && <Badge>Closed</Badge>}
          </span>
        }
        description={<span className="font-mono text-xs">{run.id}</span>}
        actions={view.stop.can && <StopButton path={view.stop.path} />}
      />
      {!view.event_store.available && (
        <Card className="border-destructive/40" data-notice="event-store-error">
          <CardContent className="flex items-start gap-2 py-4 text-sm text-destructive">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
            {view.event_store.error}
          </CardContent>
        </Card>
      )}
      {view.event_store.available && <div className="space-y-4">
        {view.stop.sent_state && view.stop.sent_state !== run.state && (
          <div className="border border-info/40 bg-info/10 px-3 py-2 text-sm" data-notice="stop-sent">
            The Dispatcher says that the Run is {stateText(view.stop.sent_state).toLowerCase()}. The stop event shows here when it arrives.
          </div>
        )}
        {view.take_over && <TakeOver takeOver={view.take_over} />}
        <Facts view={view} />
        <Executions executions={view.executions} />
        {view.report ? <Report report={view.report} /> : <Section title="Report"><None text="No report yet." /></Section>}
        <Events title="Interventions" events={view.interventions} empty="No person steered this Run." />
        <Events title="Events" events={view.events} empty="No events." />
        <Commands commands={view.commands} />
      </div>}
    </DashboardLayout>
  )
}

function StopButton({ path }: { path: string }) {
  const stop = () => {
    if (window.confirm('Stop this Run? Its Execution ends, and the Run is stopped.')) router.post(path)
  }
  return (
    <Button variant="destructive" onClick={stop} data-action="stop-run">
      <Square className="h-4 w-4" aria-hidden /> Stop
    </Button>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Card data-section={title.toLowerCase()}>
      <CardHeader className="pb-2"><CardTitle className="text-base">{title}</CardTitle></CardHeader>
      <CardContent className="text-sm">{children}</CardContent>
    </Card>
  )
}

function TakeOver({ takeOver }: { takeOver: RunTakeOver }) {
  return (
    <Section title="Take-over">
      <p className="mb-2">
        {actorText(takeOver.by)} took over this Run. Its Agent session continues in a terminal on{' '}
        <span className="font-mono text-xs" data-take-over="vm">{takeOver.vm ?? 'its VM'}</span>.
      </p>
      {takeOver.attach
        ? <div><span className="text-muted-foreground">Attach with: </span><code className="bg-muted px-1.5 py-0.5 text-xs break-all" data-take-over="attach">{takeOver.attach}</code></div>
        : <None text="The attach command is not known yet." />}
    </Section>
  )
}

function Facts({ view }: { view: RunProps }) {
  const run = view.run
  const facts: [string, React.ReactNode][] = [
    ['Reason', run.reason || <None />],
    ['Trigger', run.trigger || <None />],
    ['Started by', actorText(run.actor)],
    ['Project', view.project?.name ?? (run.project || <None />)],
    ['Repository', <span className="font-mono text-xs">{run.repository}</span>],
    ['Folder', run.folder ? <span className="font-mono text-xs">{run.folder}</span> : <None text="The root" />],
    ['Base branch', run.base_branch ? <span className="font-mono text-xs">{run.base_branch}</span> : <None text="The default branch" />],
    ['PR', run.pr.url
      ? <span><a href={run.pr.url} className="underline underline-offset-2" target="_blank" rel="noreferrer">{run.pr.url}</a>{run.pr.outcome ? ` (${run.pr.outcome})` : ' (open)'}</span>
      : <None text="No PR" />],
    ['Agent sessions', view.agent_sessions.length
      ? <span className="flex flex-col">{view.agent_sessions.map(s => <Link key={s.id} href={s.path} className="font-mono text-xs underline underline-offset-2">{s.id}</Link>)}</span>
      : <None />],
  ]
  return (
    <Section title="Run">
      <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-[10rem_1fr]">
        {facts.map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="min-w-0 break-words">{value}</dd>
          </div>
        ))}
      </dl>
    </Section>
  )
}

function Executions({ executions }: { executions: RunExecution[] }) {
  return (
    <Section title="Executions">
      {executions.length === 0 ? <None text="No Execution yet." /> : (
        <div className="overflow-x-auto border">
          <Table aria-label="Executions">
            <TableHeader>
              <TableRow>
                <TableHead>Start</TableHead>
                <TableHead>End</TableHead>
                <TableHead>Duration</TableHead>
                <TableHead>Reason</TableHead>
                <TableHead>Account</TableHead>
                <TableHead>VM</TableHead>
                <TableHead>Form</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {executions.map(e => (
                <TableRow key={e.id} data-execution={e.id}>
                  <TableCell className="font-mono text-xs whitespace-nowrap">{when(e.start)}</TableCell>
                  <TableCell className="font-mono text-xs whitespace-nowrap">{when(e.end)}</TableCell>
                  <TableCell className="text-xs">{durationText(e.duration_seconds)}</TableCell>
                  <TableCell className="text-xs">{e.reason ?? <None text="-" />}{e.exit_code !== null && <span className="text-muted-foreground"> (exit code {e.exit_code})</span>}</TableCell>
                  <TableCell className="font-mono text-xs">{e.account ?? '-'}</TableCell>
                  <TableCell className="font-mono text-xs">{e.vm ?? '-'}</TableCell>
                  <TableCell className="text-xs">{e.form === 'interactive' ? 'Take-over' : 'Headless'}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Section>
  )
}

function Report({ report }: { report: RunReport }) {
  const images = report.evidence.filter(e => e.image)
  const files = report.evidence.filter(e => !e.image)
  return (
    <Section title="Report">
      <div className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={report.outcome === 'done' ? 'ok' : report.outcome === 'failed' ? 'error' : 'warn'}>{eventKindText(report.outcome || 'report')}</Badge>
          {report.no_evidence && <Badge variant="warn" data-flag="no-evidence">no Evidence</Badge>}
        </div>
        {report.summary && <p>{report.summary}</p>}
        {report.question && <p><span className="text-muted-foreground">Question: </span>{report.question}</p>}
        {report.cause_report && (
          <div>
            <h3 className="mb-1 font-medium">Cause report</h3>
            <p className="whitespace-pre-wrap">{report.cause_report}</p>
          </div>
        )}
        <div>
          <h3 className="mb-1 font-medium">Checks</h3>
          {report.checks.length === 0 ? <None text="No checks." /> : (
            <ul className="space-y-2">
              {report.checks.map((c, i) => (
                <li key={`${c.name}-${i}`} data-check={c.name ?? c.command} className="border p-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant={c.passed ? 'ok' : 'error'}>{checkText(c)}</Badge>
                    <span className="font-medium">{c.name}</span>
                    <code className="text-xs text-muted-foreground">{c.command}</code>
                  </div>
                  {c.output_tail && <pre className="mt-2 max-h-64 overflow-auto bg-muted p-2 text-xs whitespace-pre-wrap">{c.output_tail}</pre>}
                </li>
              ))}
            </ul>
          )}
        </div>
        <div>
          <h3 className="mb-1 font-medium">Evidence</h3>
          {report.evidence.length === 0 && <None text="No Evidence files." />}
          {images.map(e => (
            <figure key={e.path} className="mb-3">
              <img src={e.url} alt={e.path} className="max-h-[32rem] max-w-full border" data-evidence={e.path} />
              <figcaption className="font-mono text-xs text-muted-foreground">{e.path}</figcaption>
            </figure>
          ))}
          {files.length > 0 && (
            <ul className="list-disc pl-5">
              {files.map(e => <li key={e.path}><a href={e.url} className="font-mono text-xs underline underline-offset-2">{e.path}</a></li>)}
            </ul>
          )}
        </div>
        <div>
          <h3 className="mb-1 font-medium">evidence/report.md</h3>
          {report.report_md !== null
            ? <pre className="max-h-[40rem] overflow-auto bg-muted p-3 text-xs whitespace-pre-wrap" data-evidence="report.md">{report.report_md}</pre>
            : <None text={report.report_md_error ?? 'No evidence/report.md.'} />}
        </div>
      </div>
    </Section>
  )
}

function Events({ title, events, empty }: { title: string; events: RunEvent[]; empty: string }) {
  return (
    <Section title={title}>
      {events.length === 0 ? <None text={empty} /> : (
        <ol className="space-y-1">
          {events.map(e => (
            <li key={`${title}-${e.seq}`} data-event={e.kind} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5">
              <span className="font-mono text-xs whitespace-nowrap text-muted-foreground">{when(e.time)}</span>
              <span className="font-medium">{eventKindText(e.kind)}</span>
              {e.intervention && <span>by {actorText(e.actor)}</span>}
              {e.detail && <span className="min-w-0 break-words text-muted-foreground">{e.detail}</span>}
              {e.reason && <span className="text-xs text-muted-foreground">({e.reason})</span>}
            </li>
          ))}
        </ol>
      )}
    </Section>
  )
}

function Commands({ commands }: { commands: RunProps['commands'] }) {
  const rows: [string, string][] = [['Message the Run', commands.message], ['Take over the Run', commands.take_over], ['Stop the Run', commands.stop]]
  return (
    <Section title="CLI commands">
      <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-[10rem_1fr]">
        {rows.map(([label, command]) => (
          <div key={label} className="contents">
            <dt className="text-muted-foreground">{label}</dt>
            <dd><code className="bg-muted px-1.5 py-0.5 text-xs break-all">{command}</code></dd>
          </div>
        ))}
      </dl>
    </Section>
  )
}
