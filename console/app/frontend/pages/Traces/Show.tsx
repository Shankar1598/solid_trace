import { Link } from '@inertiajs/react'
import { AlertTriangle } from 'lucide-react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { clockTime, durationText, type Trace, type TraceSpan } from '@/lib/traces'

/**
 * One trace across the Organization's Projects: a waterfall of its Spans,
 * each under its parent, with its Project, op, description, start,
 * duration, self time and status, repeats collapsed and the critical path
 * marked; then its Issues and its Logs. A trace that wasn't sampled shows
 * its Logs and Issues alone. Console's TraceView builds it.
 */
export default function TraceShow({ trace }: { trace: Trace }) {
  const { spans } = trace
  return (
    <DashboardLayout>
      <div className="mx-auto max-w-[1600px] space-y-6">
        <PageHeader
          title="Trace"
          tabTitle={`Trace ${trace.trace_id.slice(0, 8)}`}
          description={
            <span>
              <span className="font-mono">{trace.trace_id}</span>
              {spans.length > 0 && ` · ${trace.span_count} Spans over ${durationText(trace.duration_us)}`}
            </span>
          }
        />

        {!trace.event_store.available && (
          <div role="alert" className="flex items-start gap-2 border border-warning/40 px-3 py-2 text-sm">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
            <span>{trace.event_store.error}</span>
          </div>
        )}

        {trace.sampled === false && (
          <p className="text-sm text-muted-foreground" data-notice="unsampled">
            {trace.logs.length + trace.issues.length > 0
              ? "This trace wasn't sampled: its SDK sent none of its Spans. Its Issues and Logs are below."
              : "None of this Organization's Projects has Spans, Logs or Issues of this trace."}
            {trace.time === null && trace.logs.length === 0 && ' A link from one of its Events or Logs finds its Logs by their time.'}
          </p>
        )}

        {spans.length > 0 && <Waterfall trace={trace} />}
        {trace.issues.length > 0 && <Issues trace={trace} />}
        {trace.logs.length > 0 && <Logs trace={trace} />}
      </div>
    </DashboardLayout>
  )
}

function Section({ title, aside, children }: { title: string; aside?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section aria-label={title} className="space-y-2">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-sm font-medium">{title}</h2>
        {aside}
      </div>
      {children}
    </section>
  )
}

const WATERFALL_COLUMNS = 'grid grid-cols-[minmax(0,2fr)_8rem_5rem_5rem_5rem_6rem_minmax(0,1.5fr)] gap-2'

function Waterfall({ trace }: { trace: Trace }) {
  const total = Math.max(trace.duration_us, 1)
  return (
    <div role="table" aria-label="Spans" className="overflow-x-auto border text-xs">
      <div role="row" className={`${WATERFALL_COLUMNS} min-w-[56rem] border-b bg-muted/40 px-2 py-1 font-medium text-muted-foreground`}>
        <span role="columnheader">Span</span>
        <span role="columnheader">Project</span>
        <span role="columnheader" className="text-right">Start</span>
        <span role="columnheader" className="text-right">Duration</span>
        <span role="columnheader" className="text-right">Self</span>
        <span role="columnheader">Status</span>
        <span role="columnheader">
          Timeline <span className="font-normal">· <span className="text-foreground">▍</span> critical path</span>
        </span>
      </div>
      {trace.spans.map(span => (
        <SpanRow key={`${span.project.id}/${span.span_id}`} span={span} total={total} />
      ))}
    </div>
  )
}

function SpanRow({ span, total }: { span: TraceSpan; total: number }) {
  const left = (span.offset_us / total) * 100
  const width = Math.max((span.span_us / total) * 100, 0.3)
  const failed = span.status !== '' && span.status !== 'ok'
  const repeated = span.count > 1
  return (
    <div
      role="row"
      data-span={span.span_id}
      data-critical={span.critical || undefined}
      className={`${WATERFALL_COLUMNS} min-w-[56rem] items-center border-b border-l-2 px-2 py-1 last:border-b-0 ${span.critical ? 'border-l-foreground' : 'border-l-transparent'}`}
    >
      <span role="cell" className="flex min-w-0 items-baseline gap-2" style={{ paddingLeft: `${span.depth * 1}rem` }}>
        <span className={span.is_segment ? 'shrink-0 font-semibold' : 'shrink-0 font-medium'}>{span.op || 'span'}</span>
        {repeated && (
          <span className="shrink-0 border border-warning/50 px-1 text-[11px] text-warning" title={`${span.count} Spans of one group, durations totalled`}>
            ×{span.count}
          </span>
        )}
        <span className="truncate text-muted-foreground" title={span.description}>{span.description}</span>
      </span>
      <span role="cell" className="truncate">{span.project.name}</span>
      <span role="cell" className="text-right tabular-nums">{durationText(span.offset_us)}</span>
      <span role="cell" className="text-right tabular-nums" title={repeated ? 'Total of the repeats' : undefined}>
        {durationText(span.duration_us)}
      </span>
      <span role="cell" className="text-right tabular-nums">{durationText(span.self_time_us)}</span>
      <span role="cell" className={failed ? 'text-destructive' : 'text-muted-foreground'}>{span.status}</span>
      <span role="cell" className="relative h-3 bg-muted/30" aria-hidden>
        <span
          className={`absolute inset-y-0 ${failed ? 'bg-destructive' : span.is_segment ? 'bg-chart-1' : 'bg-chart-2'} ${repeated ? 'opacity-60' : ''}`}
          style={{ left: `${left}%`, width: `${Math.min(width, 100 - left)}%` }}
        />
      </span>
    </div>
  )
}

function Issues({ trace }: { trace: Trace }) {
  return (
    <Section title="Issues">
      <ul aria-label="Issues" className="divide-y border text-sm">
        {trace.issues.map(issue => (
          <li key={issue.id} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-3 py-2">
            <Link href={issue.url} className="min-w-0 truncate font-medium hover:underline">{issue.title}</Link>
            <span className="text-xs text-muted-foreground">{issue.project.name}</span>
            <span className="text-xs text-muted-foreground">
              {issue.events} {issue.events === 1 ? 'Event' : 'Events'} in this trace
            </span>
            {issue.status === 'resolved' && <span className="text-xs text-muted-foreground">resolved</span>}
          </li>
        ))}
      </ul>
    </Section>
  )
}

function Logs({ trace }: { trace: Trace }) {
  return (
    <Section
      title="Logs"
      aside={
        <span className="flex flex-wrap gap-3 text-xs">
          {trace.more_logs && <span className="text-muted-foreground">The first {trace.logs.length} shown.</span>}
          {trace.logs_links.map(({ project, url }) => (
            <a key={project.id} href={url} className="text-muted-foreground hover:text-foreground hover:underline">
              {project.name}'s in Logs
            </a>
          ))}
        </span>
      }
    >
      <ol aria-label="Logs" className="divide-y border font-mono text-xs">
        {trace.logs.map(log => (
          <li key={`${log.project.id}/${log.id}`}>
            <a href={log.url} className="flex min-w-0 gap-3 px-3 py-1.5 hover:bg-muted/50">
              <span className="shrink-0 tabular-nums text-muted-foreground" title={log.time}>{clockTime(log.time)}</span>
              <span className="w-24 shrink-0 truncate font-sans text-muted-foreground">{log.project.name}</span>
              {log.level && <span className="w-12 shrink-0 uppercase text-muted-foreground">{log.level}</span>}
              <span className="min-w-0 truncate">{log.message}</span>
            </a>
          </li>
        ))}
      </ol>
    </Section>
  )
}
