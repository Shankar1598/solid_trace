import { useState } from 'react'
import { Link, usePage } from '@inertiajs/react'
import { AlertTriangle, ArrowDown, ArrowUp } from 'lucide-react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { durationText } from '@/lib/traces'
import {
  DEFAULT_SORT,
  changeText,
  nextSort,
  percentText,
  performancePath,
  rangeOfAnswer,
  sortTransactions,
  throughputText,
  transactionHref,
  worse,
  type NumberKey,
  type Performance,
  type Sort,
  type SortKey,
  type TransactionRow,
} from '@/lib/performance'
import { cn } from '@/lib/utils'
import type { SharedProps } from '@/types'
import { PerformanceFilters } from './components/PerformanceFilters'

const TRACES_DOCS = 'https://github.com/solidtrace/solid_trace/blob/main/docs/traces.md'

const COLUMNS: { key: SortKey; label: string; title?: string }[] = [
  { key: 'label', label: 'Transaction' },
  { key: 'throughput_per_minute', label: 'Per minute', title: 'Requests a minute, estimated: each sampled request counts as 1 / its sample rate' },
  { key: 'p50_us', label: 'p50' },
  { key: 'p95_us', label: 'p95' },
  { key: 'p99_us', label: 'p99' },
  { key: 'error_rate', label: 'Errors', title: 'The share of requests whose status was sent and isn\'t ok, weighted by sample rate' },
  { key: 'time_consumed_us', label: 'Time consumed', title: 'Requests × their average duration, estimated as throughput is' },
]

/**
 * A Project's Performance: one row per Transaction name over the range and
 * environment in the URL, with throughput, percentiles, error rate and time
 * consumed, each with its change from the period before. Sorted by time
 * consumed until a column's header is clicked. Console's TransactionsView
 * builds it, as it does for list_transactions.
 */
export default function PerformanceShow({ performance }: { performance: Performance }) {
  const org = usePage<SharedProps>().props.current_org?.slug ?? ''
  const { project, transactions } = performance
  const [sort, setSort] = useState<Sort>(DEFAULT_SORT)
  const rows = sortTransactions(transactions, sort)
  const range = rangeOfAnswer(performance.range)
  return (
    <DashboardLayout>
      <div className="mx-auto max-w-[1600px]">
        <PageHeader
          className="mb-4"
          title="Performance"
          tabTitle={`Performance · ${project.name}`}
          breadcrumbs={[{ label: 'Projects', href: `/${org}/projects` }, { label: project.name, href: `/${org}/projects/${project.slug}` }]}
          description="The requests and jobs this Project's Sentry SDKs traced. Throughput and time consumed are estimates, scaled up by each request's sample rate."
          actions={
            <PerformanceFilters
              path={performancePath(org, project.slug)}
              range={performance.range}
              environment={performance.environment}
              environments={performance.environments}
            />
          }
        />

        {!performance.event_store.available && (
          <div role="alert" className="mb-3 flex items-start gap-2 border border-warning/40 px-3 py-2 text-sm">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
            <span>{performance.event_store.error}</span>
          </div>
        )}

        {performance.event_store.available && transactions.length === 0 ? (
          <p className="text-sm text-muted-foreground" data-notice="no-transactions">
            No Transactions in this range. To see where requests spend their time, turn tracing on in the Project's
            Sentry SDK: the <a href={TRACES_DOCS} target="_blank" rel="noreferrer" className="font-medium text-foreground underline underline-offset-2">Traces docs</a> say how.
          </p>
        ) : (
          transactions.length > 0 && (
            <Table aria-label="Transactions">
              <TableHeader>
                <TableRow>
                  {COLUMNS.map(column => (
                    <TableHead
                      key={column.key}
                      className={column.key === 'label' ? undefined : 'text-right'}
                      aria-sort={sort.key === column.key ? (sort.descending ? 'descending' : 'ascending') : undefined}
                    >
                      <button
                        type="button"
                        title={column.title}
                        onClick={() => setSort(nextSort(sort, column.key))}
                        className={cn('inline-flex items-center gap-1 hover:text-foreground', column.key !== 'label' && 'flex-row-reverse')}
                      >
                        {column.label}
                        {sort.key === column.key && (sort.descending ? <ArrowDown className="h-3 w-3" /> : <ArrowUp className="h-3 w-3" />)}
                      </button>
                    </TableHead>
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map(row => (
                  <TransactionTableRow
                    key={row.name}
                    row={row}
                    href={transactionHref(org, project.slug, row.name, range, performance.environment)}
                  />
                ))}
              </TableBody>
            </Table>
          )
        )}
      </div>
    </DashboardLayout>
  )
}

function TransactionTableRow({ row, href }: { row: TransactionRow; href: string }) {
  return (
    <TableRow data-transaction={row.name}>
      <TableCell className="max-w-[28rem]">
        <Link href={href} className={cn('block truncate font-medium hover:underline', row.label !== row.name && 'italic')} title={row.name}>
          {row.label}
        </Link>
        <span className="text-xs text-muted-foreground">{row.sampled.toLocaleString()} sampled</span>
      </TableCell>
      <NumberCell row={row} number="throughput_per_minute" text={`~${throughputText(row.throughput_per_minute)}`} />
      <NumberCell row={row} number="p50_us" text={durationText(row.p50_us)} />
      <NumberCell row={row} number="p95_us" text={durationText(row.p95_us)} />
      <NumberCell row={row} number="p99_us" text={durationText(row.p99_us)} />
      <NumberCell row={row} number="error_rate" text={percentText(row.error_rate)} />
      <NumberCell row={row} number="time_consumed_us" text={`~${durationText(row.time_consumed_us)}`} />
    </TableRow>
  )
}

/** A number, with its change from the period before under it. */
function NumberCell({ row, number, text }: { row: TransactionRow; number: NumberKey; text: string }) {
  const change = row.change?.[number]
  const label = row.previous ? changeText(number, change) : 'new'
  return (
    <TableCell className="text-right font-mono tabular-nums whitespace-nowrap" data-number={number}>
      <div>{text}</div>
      {label && (
        <div className={cn('text-xs', worse(number, change) ? 'text-destructive' : 'text-muted-foreground')} title="Change from the period before">
          {label}
        </div>
      )}
    </TableCell>
  )
}
