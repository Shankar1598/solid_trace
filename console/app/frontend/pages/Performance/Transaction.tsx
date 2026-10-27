import type { ReactNode } from 'react'
import { Link, usePage } from '@inertiajs/react'
import { AlertTriangle, Timer } from 'lucide-react'
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts'
import { format } from 'date-fns'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { OverviewSection } from '@/components/overview/OverviewSection'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { valueText } from '@/lib/charts/axis'
import { durationText, microsecondsText } from '@/lib/traces'
import {
  exactPercentText,
  percentText,
  performancePath,
  performanceSearch,
  rangeOfAnswer,
  throughputText,
  type ReleaseDurations,
  type SelfTime,
  type Transaction,
  type TransactionNumbers,
} from '@/lib/performance'
import { cn } from '@/lib/utils'
import type { SharedProps } from '@/types'
import { PerformanceFilters } from './components/PerformanceFilters'

// Releases the chart draws in a colour of their own; more share the last.
const COLORED_RELEASES = 8

/**
 * One Transaction over the range and environment in the URL: its p50 and
 * p95 by release, where its time goes by Span op and by group, the groups a
 * request repeats, and its slowest traces, each linking to the trace page.
 * Its numbers lead in a strip, and each part is a panel as on the
 * Overview. Console's TransactionView builds it, as it does for
 * get_transaction.
 */
export default function PerformanceTransaction({ transaction }: { transaction: Transaction }) {
  const org = usePage<SharedProps>().props.current_org?.slug ?? ''
  const { project, summary } = transaction
  const back = `${performancePath(org, project.slug)}${performanceSearch(rangeOfAnswer(transaction.range), transaction.environment)}`
  return (
    <DashboardLayout>
      <PageHeader
        title={<span className={transaction.label !== transaction.name ? 'italic' : undefined}>{transaction.label}</span>}
        tabTitle={`${transaction.label} · ${project.name}`}
        breadcrumbs={[{ label: 'Performance', href: back }, { label: project.name }]}
      />
      <div className="space-y-4">
        <PerformanceFilters
          path={`${performancePath(org, project.slug)}/transaction`}
          range={transaction.range}
          environment={transaction.environment}
          environments={[]}
          more={{ name: transaction.name }}
        />

        {!transaction.event_store.available && (
          <div role="alert" className="flex items-start gap-2 border border-warning/40 px-3 py-2 text-sm">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
            <span>{transaction.event_store.error}</span>
          </div>
        )}

        {transaction.event_store.available && (!summary || summary.sampled === 0) ? (
          <Card className="border-dashed" data-notice="no-requests">
            <CardContent className="flex flex-col items-center justify-center py-12 text-center">
              <Timer className="mb-4 h-12 w-12 text-muted-foreground opacity-50" aria-hidden />
              <h2 className="text-lg font-medium">No requests in this range</h2>
              <p className="max-w-md text-sm text-muted-foreground">Pick a longer range or another environment to see this Transaction's requests.</p>
            </CardContent>
          </Card>
        ) : transaction.event_store.available && summary && (
          <>
            <SummaryStrip summary={summary} />
            <div className="grid grid-cols-1 gap-3 xl:grid-cols-12">
              <OverviewSection id="releases" className="xl:col-span-8" title="p95 by release" description="The p95 of the sampled requests over the range, a line per release.">
                <ReleaseChart releases={transaction.releases} />
              </OverviewSection>
              <OverviewSection id="release-table" className="xl:col-span-4" bodyClassName="" title="Releases" description="p50 and p95 of the sampled requests, by release.">
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
                        <Num exact={valueText(r.sampled)}>{valueText(r.sampled)}</Num>
                        <Num exact={microsecondsText(r.p50_us)}>{durationText(r.p50_us)}</Num>
                        <Num exact={microsecondsText(r.p95_us)}>{durationText(r.p95_us)}</Num>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </OverviewSection>

              <OverviewSection id="ops" className="xl:col-span-5" bodyClassName="" title="Self time by op"
                description="Self time is a Span's duration less its children's: its share of the Transaction's time, and on average a request.">
                <SelfTimes label="By op" rows={transaction.ops.map(o => ({ key: o.op, name: o.op, ...o }))} />
              </OverviewSection>
              <OverviewSection id="groups" className="xl:col-span-7" bodyClassName="" title="Self time by group"
                description="Spans grouped by op and description, with literals taken out: their share of the Transaction's time, and on average a request.">
                <SelfTimes
                  label="By group"
                  rows={transaction.groups.map(g => ({ key: g.group, name: g.op, detail: g.normalised, title: g.description, ...g }))}
                />
              </OverviewSection>

              {transaction.repeats.length > 0 && (
                <OverviewSection id="repeats" className="xl:col-span-12" bodyClassName="" title="Repeated Spans"
                  description="Spans a request runs more than once, such as an N+1 query: how many times, in the requests that ran them.">
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
                          <TableCell className="max-w-[40rem] whitespace-normal">
                            <span className="mr-2 font-medium">{r.op}</span>
                            <span className="break-all font-mono text-xs text-muted-foreground" title={r.description}>{r.normalised}</span>
                          </TableCell>
                          <Num exact={valueText(r.median)}>×{valueText(r.median)}</Num>
                          <Num exact={valueText(r.max)}>×{valueText(r.max)}</Num>
                          <Num exact={valueText(r.sampled)}>{valueText(r.sampled)}</Num>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </OverviewSection>
              )}

              <OverviewSection id="slowest" className="xl:col-span-12" bodyClassName="" title="Slowest traces"
                description="This Transaction's slowest sampled requests. Open one to see its Spans, Logs and Issues.">
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
                        <TableCell className="whitespace-nowrap font-mono text-xs">{format(new Date(t.start), 'MMM d, HH:mm:ss')}</TableCell>
                        <Num exact={microsecondsText(t.duration_us)}>{durationText(t.duration_us)}</Num>
                        <TableCell>
                          {t.status && t.status !== 'ok' ? <Badge variant="error" dot>{t.status}</Badge> : <span className="text-muted-foreground">{t.status || '—'}</span>}
                        </TableCell>
                        <TableCell className="font-mono text-xs">{t.release}</TableCell>
                        <TableCell>{t.environment}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </OverviewSection>
            </div>
          </>
        )}
      </div>
    </DashboardLayout>
  )
}

/** The Transaction's numbers over the range, each in full in its title. */
function SummaryStrip({ summary }: { summary: TransactionNumbers }) {
  const stats: { label: string; text: string; exact: string; bad?: boolean }[] = [
    { label: 'Sampled', text: valueText(summary.sampled), exact: valueText(summary.sampled) },
    { label: 'Per minute', text: `~${throughputText(summary.throughput_per_minute)}`, exact: `${valueText(summary.throughput_per_minute)} a minute, estimated` },
    { label: 'p50', text: durationText(summary.p50_us), exact: microsecondsText(summary.p50_us) },
    { label: 'p95', text: durationText(summary.p95_us), exact: microsecondsText(summary.p95_us) },
    { label: 'p99', text: durationText(summary.p99_us), exact: microsecondsText(summary.p99_us) },
    { label: 'Errors', text: percentText(summary.error_rate), exact: exactPercentText(summary.error_rate), bad: summary.error_rate > 0 },
  ]
  return (
    <dl className="grid grid-cols-3 border sm:grid-cols-6" data-summary>
      {stats.map(stat => (
        <div key={stat.label} className="space-y-0.5 border-b border-r px-3 py-2 sm:border-b-0 [&:nth-child(3n)]:border-r-0 sm:[&:nth-child(3n)]:border-r sm:last:border-r-0 [&:nth-child(n+4)]:border-b-0">
          <dt className="text-xs text-muted-foreground">{stat.label}</dt>
          <dd className={cn('font-mono text-base font-semibold tabular-nums', stat.bad && 'text-destructive')} title={stat.exact}>{stat.text}</dd>
        </div>
      ))}
    </dl>
  )
}

/** A number cell, the number in full in its title. */
function Num({ exact, children }: { exact: string; children: ReactNode }) {
  return <TableCell className="text-right font-mono tabular-nums whitespace-nowrap" title={exact}>{children}</TableCell>
}

/** Self times as bars of their share. */
function SelfTimes({ label, rows }: {
  label: string
  rows: (SelfTime & { key: string; name: string; detail?: string; title?: string })[]
}) {
  return (
    <Table aria-label={`Self time ${label.toLowerCase()}`} className="table-fixed">
      <TableHeader>
        <TableRow>
          <TableHead>{label === 'By op' ? 'Op' : 'Group'}</TableHead>
          <TableHead className="w-[22%]"><span className="sr-only">Share as a bar</span></TableHead>
          <TableHead className="w-20 text-right">Share</TableHead>
          <TableHead className="w-24 text-right">A request</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map(row => (
          <TableRow key={row.key}>
            <TableCell className="truncate" title={row.title ?? row.name}>
              <span className="font-medium">{row.name}</span>
              {row.detail && <span className="ml-2 font-mono text-xs text-muted-foreground">{row.detail}</span>}
            </TableCell>
            <TableCell aria-hidden>
              <div className="h-1.5 bg-muted">
                <div className="h-full bg-chart-1" style={{ width: `${Math.min(row.share * 100, 100)}%` }} />
              </div>
            </TableCell>
            <Num exact={exactPercentText(row.share)}>{percentText(row.share)}</Num>
            <Num exact={microsecondsText(row.avg_self_time_us)}>{durationText(row.avg_self_time_us)}</Num>
          </TableRow>
        ))}
      </TableBody>
    </Table>
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
      <ChartContainer config={config} className="aspect-auto w-full" style={{ height: 220 }}>
        <LineChart data={data} margin={{ top: 8, right: 12, bottom: 0, left: 0 }}>
          <CartesianGrid vertical={false} stroke="var(--border)" />
          <XAxis dataKey="time" tickLine={false} tick={{ fontSize: 11 }} minTickGap={24} tickFormatter={t => format(new Date(t), 'MMM d HH:mm')} />
          <YAxis width={64} tickLine={false} axisLine={false} tick={{ fontSize: 11 }} tickFormatter={ms => durationText(ms * 1000)} />
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
