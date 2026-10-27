import { AlertTriangle } from 'lucide-react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { durationText, type Trace, type TraceSpan } from '@/lib/traces'

/**
 * One trace across the Organization's Projects: a waterfall of its Spans,
 * each under its parent, with its Project, op, description, start,
 * duration, self time and status. Console's TraceView builds it.
 */
export default function TraceShow({ trace }: { trace: Trace }) {
  const { spans } = trace
  return (
    <DashboardLayout>
      <div className="mx-auto max-w-[1600px]">
        <PageHeader
          className="mb-4"
          title="Trace"
          tabTitle={`Trace ${trace.trace_id.slice(0, 8)}`}
          description={
            <span>
              <span className="font-mono">{trace.trace_id}</span>
              {spans.length > 0 && ` · ${spans.length} Spans over ${durationText(trace.duration_us)}`}
            </span>
          }
        />

        {!trace.event_store.available && (
          <div role="alert" className="mb-3 flex items-start gap-2 border border-warning/40 px-3 py-2 text-sm">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
            <span>{trace.event_store.error}</span>
          </div>
        )}

        {trace.event_store.available && spans.length === 0 ? (
          <p className="text-sm text-muted-foreground" data-notice="no-spans">
            None of this Organization's Projects has Spans of this trace.
          </p>
        ) : (
          spans.length > 0 && <Waterfall trace={trace} />
        )}
      </div>
    </DashboardLayout>
  )
}

function Waterfall({ trace }: { trace: Trace }) {
  const total = Math.max(trace.duration_us, 1)
  return (
    <div role="table" aria-label="Spans" className="border text-xs">
      <div role="row" className="grid grid-cols-[minmax(0,2fr)_8rem_5rem_5rem_5rem_6rem_minmax(0,1.5fr)] gap-2 border-b bg-muted/40 px-2 py-1 font-medium text-muted-foreground">
        <span role="columnheader">Span</span>
        <span role="columnheader">Project</span>
        <span role="columnheader" className="text-right">Start</span>
        <span role="columnheader" className="text-right">Duration</span>
        <span role="columnheader" className="text-right">Self</span>
        <span role="columnheader">Status</span>
        <span role="columnheader">Timeline</span>
      </div>
      {trace.spans.map(span => (
        <SpanRow key={`${span.project.id}/${span.span_id}`} span={span} total={total} />
      ))}
    </div>
  )
}

function SpanRow({ span, total }: { span: TraceSpan; total: number }) {
  const left = (span.offset_us / total) * 100
  const width = Math.max((span.duration_us / total) * 100, 0.3)
  const failed = span.status !== '' && span.status !== 'ok'
  return (
    <div
      role="row"
      data-span={span.span_id}
      className="grid grid-cols-[minmax(0,2fr)_8rem_5rem_5rem_5rem_6rem_minmax(0,1.5fr)] items-center gap-2 border-b px-2 py-1 last:border-b-0"
    >
      <span role="cell" className="flex min-w-0 items-baseline gap-2" style={{ paddingLeft: `${span.depth * 1}rem` }}>
        <span className={span.is_segment ? 'shrink-0 font-semibold' : 'shrink-0 font-medium'}>{span.op || 'span'}</span>
        <span className="truncate text-muted-foreground" title={span.description}>{span.description}</span>
      </span>
      <span role="cell" className="truncate">{span.project.name}</span>
      <span role="cell" className="text-right tabular-nums">{durationText(span.offset_us)}</span>
      <span role="cell" className="text-right tabular-nums">{durationText(span.duration_us)}</span>
      <span role="cell" className="text-right tabular-nums">{durationText(span.self_time_us)}</span>
      <span role="cell" className={failed ? 'text-destructive' : 'text-muted-foreground'}>{span.status}</span>
      <span role="cell" className="relative h-3 bg-muted/30" aria-hidden>
        <span
          className={`absolute inset-y-0 ${failed ? 'bg-destructive' : span.is_segment ? 'bg-chart-1' : 'bg-chart-2'}`}
          style={{ left: `${left}%`, width: `${Math.min(width, 100 - left)}%` }}
        />
      </span>
    </div>
  )
}
