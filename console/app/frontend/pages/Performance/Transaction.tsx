import type { ReactNode } from 'react'
import { Link, usePage } from '@inertiajs/react'
import { AlertTriangle } from 'lucide-react'
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts'
import { format } from 'date-fns'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { durationText } from '@/lib/traces'
import {
  percentText,
  performancePath,
  performanceSearch,
  rangeOfAnswer,
  throughputText,
  type ReleaseDurations,
  type SelfTime,
  type Transaction,
} from '@/lib/performance'
import type { SharedProps } from '@/types'
import { PerformanceFilters } from './components/PerformanceFilters'

// Releases the chart draws in a colour of their own; more share the last.
const COLORED_RELEASES = 8

/**
 * One Transaction over the range and environment in the URL: its p50 and
 * p95 by release, where its time goes by Span op and by group, the groups a
 * request repeats, and its slowest traces, each linking to the trace page.
 * Console's TransactionView builds it, as it does for get_transaction.
 */
export default function PerformanceTransaction({ transaction }: { transaction: Transaction }) {
  const org = usePage<SharedProps>().props.current_org?.slug ?? ''
  const { project, summary } = transaction
  const back = `${performancePath(org, project.slug)}${performanceSearch(rangeOfAnswer(transaction.range), transaction.environment)}`
  return (
    <DashboardLayout>
      <div className="mx-auto max-w-[1600px] space-y-6">
        <PageHeader
          className="mb-0"
          title={<span className={transaction.label !== transaction.name ? 'italic' : undefined}>{transaction.label}</span>}
          tabTitle={`${transaction.label} · ${project.name}`}
          breadcrumbs={[
            { label: 'Projects', href: `/${org}/projects` },
            { label: project.name, href: `/${org}/projects/${project.slug}` },
            { label: 'Performance', href: back },
          ]}
          description={summary && (
            <span data-summary>
              {summary.sampled.toLocaleString()} sampled · ~{throughputText(summary.throughput_per_minute)} a minute ·
              p50 {durationText(summary.p50_us)} · p95 {durationText(summary.p95_us)} · p99 {durationText(summary.p99_us)} ·
              errors {percentText(summary.error_rate)}
            </span>
          )}
          actions={
            <PerformanceFilters
              path={`${performancePath(org, project.slug)}/transaction`}
              range={transaction.range}
              environment={transaction.environment}
              environments={[]}
              more={{ name: transaction.name }}
            />
          }
        />

        {!transaction.event_store.available && (
          <div role="alert" className="flex items-start gap-2 border border-warning/40 px-3 py-2 text-sm">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
            <span>{transaction.event_store.error}</span>
          </div>
        )}

        {transaction.event_store.available && (!summary || summary.sampled === 0) ? (
          <p className="text-sm text-muted-foreground" data-notice="no-requests">No requests of this Transaction in this range.</p>
        ) : transaction.event_store.available && (
          <>
            <Section title="By release" description="p50 and p95 of the sampled requests, by release.">
              <ReleaseChart releases={transaction.releases} />
              <Table aria-label="Releases">
                <TableHeader>
                  <TableRow>
                    <TableHead>Release</TableHead>
                    <TableHead className="text-right">Sampled</TableHead>
                    <TableHead className="text-right">p50</TableHead>
                    <TableHead className="text-right">p95</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {transaction.releases.map(r => (
                    <TableRow key={r.release} data-release={r.release}>
                      <TableCell className="font-mono">{r.release || '(none)'}</TableCell>
                      <Num>{r.sampled.toLocaleString()}</Num>
                      <Num>{durationText(r.p50_us)}</Num>
                      <Num>{durationText(r.p95_us)}</Num>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Section>

            <Section title="Where the time goes" description="Self time, a Span's duration less its children's, as a share of the Transaction's time and on average a request.">
              <SelfTimes label="By op" rows={transaction.ops.map(o => ({ key: o.op, name: o.op, ...o }))} />
              <SelfTimes
                label="By group"
                rows={transaction.groups.map(g => ({ key: g.group, name: g.op, detail: g.normalised, title: g.description, ...g }))}
              />
            </Section>

            {transaction.repeats.length > 0 && (
              <Section title="Repeated Spans" description="Spans a request runs more than once, such as an N+1 query: how many times, in the requests that ran them.">
                <Table aria-label="Repeats">
                  <TableHeader>
                    <TableRow>
                      <TableHead>Span</TableHead>
                      <TableHead className="text-right">Median</TableHead>
                      <TableHead className="text-right">Most</TableHead>
                      <TableHead className="text-right">Requests</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {transaction.repeats.map(r => (
                      <TableRow key={r.group} data-repeat={r.group}>
                        <TableCell className="max-w-[40rem]">
                          <span className="mr-2 font-medium">{r.op}</span>
                          <span className="break-all font-mono text-xs text-muted-foreground" title={r.description}>{r.normalised}</span>
                        </TableCell>
                        <Num>×{r.median}</Num>
                        <Num>×{r.max}</Num>
                        <Num>{r.sampled.toLocaleString()}</Num>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Section>
            )}

            <Section title="Slowest traces" description="Its slowest sampled requests. Open one to see its Spans.">
              <Table aria-label="Slowest traces">
                <TableHeader>
                  <TableRow>
                    <TableHead>Trace</TableHead>
                    <TableHead>Started</TableHead>
                    <TableHead className="text-right">Duration</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Release</TableHead>
                    <TableHead>Environment</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {transaction.slowest.map(t => (
                    <TableRow key={t.trace_id} data-trace={t.trace_id}>
                      <TableCell>
                        <Link href={`/${org}/traces/${t.trace_id}`} className="font-mono text-xs hover:underline">{t.trace_id}</Link>
                      </TableCell>
                      <TableCell className="whitespace-nowrap">{format(new Date(t.start), 'MMM d, HH:mm:ss')}</TableCell>
                      <Num>{durationText(t.duration_us)}</Num>
                      <TableCell className={t.status && t.status !== 'ok' ? 'text-destructive' : 'text-muted-foreground'}>{t.status || '—'}</TableCell>
                      <TableCell className="font-mono text-xs">{t.release}</TableCell>
                      <TableCell>{t.environment}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Section>
          </>
        )}
      </div>
    </DashboardLayout>
  )
}

function Section({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <section className="space-y-2" aria-label={title}>
      <div>
        <h2 className="text-sm font-semibold">{title}</h2>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      {children}
    </section>
  )
}

function Num({ children }: { children: ReactNode }) {
  return <TableCell className="text-right font-mono tabular-nums whitespace-nowrap">{children}</TableCell>
}

/** Self times as bars of their share. */
function SelfTimes({ label, rows }: {
  label: string
  rows: (SelfTime & { key: string; name: string; detail?: string; title?: string })[]
}) {
  return (
    <div role="table" aria-label={`Self time ${label.toLowerCase()}`} className="border text-xs">
      <div role="row" className="grid grid-cols-[minmax(0,2fr)_minmax(0,1fr)_5rem_6rem] gap-2 border-b bg-muted/40 px-2 py-1 font-medium text-muted-foreground">
        <span role="columnheader">{label === 'By op' ? 'Op' : 'Group'}</span>
        <span role="columnheader" aria-hidden />

        <span role="columnheader" className="text-right">Share</span>
        <span role="columnheader" className="text-right">A request</span>
      </div>
      {rows.map(row => (
        <div role="row" key={row.key} className="grid grid-cols-[minmax(0,2fr)_minmax(0,1fr)_5rem_6rem] items-center gap-2 border-b px-2 py-1 last:border-b-0">
          <span role="cell" className="min-w-0 truncate" title={row.title ?? row.name}>
            <span className="font-medium">{row.name}</span>
            {row.detail && <span className="ml-2 font-mono text-muted-foreground">{row.detail}</span>}
          </span>
          <span role="cell" className="h-2 bg-muted/30" aria-hidden>
            <span className="block h-full bg-chart-1" style={{ width: `${Math.min(row.share * 100, 100)}%` }} />
          </span>
          <span role="cell" className="text-right tabular-nums">{percentText(row.share)}</span>
          <span role="cell" className="text-right tabular-nums">{durationText(row.avg_self_time_us)}</span>
        </div>
      ))}
    </div>
  )
}

/** p95 over time, a line per release. */
function ReleaseChart({ releases }: { releases: ReleaseDurations[] }) {
  if (releases.length === 0) return null
  const keys = releases.map((r, i) => ({ key: `r${i}`, name: r.release || '(none)', color: `var(--chart-${Math.min(i, COLORED_RELEASES - 1) + 1})` }))
  const config = Object.fromEntries(keys.map(k => [k.key, { label: `${k.name} p95`, color: k.color }])) satisfies ChartConfig
  const byTime = new Map<string, Record<string, number | string>>()
  releases.forEach((r, i) => {
    for (const p of r.points) {
      const row = byTime.get(p.time) ?? { time: p.time }
      row[`r${i}`] = p.p95_us / 1000
      byTime.set(p.time, row)
    }
  })
  const data = [...byTime.values()].sort((a, b) => String(a.time).localeCompare(String(b.time)))
  return (
    <div role="img" aria-label={`p95 by release: ${keys.map(k => k.name).join(', ')}`}>
      <ChartContainer config={config} className="aspect-auto w-full" style={{ height: 180 }}>
        <LineChart data={data} margin={{ top: 8, right: 12, bottom: 0, left: 0 }}>
          <CartesianGrid vertical={false} stroke="var(--border)" />
          <XAxis dataKey="time" tickLine={false} tick={{ fontSize: 11 }} minTickGap={24} tickFormatter={t => format(new Date(t), 'MMM d HH:mm')} />
          <YAxis width={56} tickLine={false} axisLine={false} tick={{ fontSize: 11 }} tickFormatter={ms => `${ms} ms`} />
          <ChartTooltip isAnimationActive={false} content={<ChartTooltipContent />} />
          {keys.map(k => (
            <Line key={k.key} dataKey={k.key} name={k.name} type="linear" stroke={`var(--color-${k.key})`} strokeWidth={2}
              dot={{ r: 3 }} connectNulls isAnimationActive={false} />
          ))}
        </LineChart>
      </ChartContainer>
    </div>
  )
}
