import { useMemo, useRef } from 'react'
import { QueryChart } from '@/components/charts/QueryChart'
import { chartOf, formFor } from '@/lib/charts/series'
import { formOfViz, type ChartOptions, type DashboardViz } from '@/lib/dashboards/dashboard'
import type { LogTable } from '@/lib/logs/table'
import { LogTableView } from '../../Logs/components/LogTableView'

/** What a chart's run has answered, as a card or the editor's preview shows it. */
export type ChartRun =
  | { status: 'running' }
  | { status: 'done'; table: LogTable; elapsedMs: number | null }
  | { status: 'error'; message: string }
  | { status: 'cancelled' }

/**
 * A chart's run drawn as its viz: a chart, a single number, or the table.
 * A table its viz can't draw shows as the table, saying so.
 */
export function ChartBody({ run, viz, options }: { run: ChartRun; viz: DashboardViz; options: ChartOptions }) {
  // Series keep their colours and hidden state across re-runs of this chart.
  const colors = useRef<ReadonlyMap<string, number>>(new Map())
  const hidden = useRef<ReadonlySet<string>>(new Set())
  const table = run.status === 'done' ? run.table : null
  const model = useMemo(() => (table ? chartOf(table) : null), [table])

  switch (run.status) {
    case 'running':
      return (
        <div role="status" aria-label="Running the chart's Query" className="flex h-full min-h-24 flex-col gap-2 p-3">
          <div className="h-3 w-1/3 animate-pulse rounded bg-muted" />
          <div className="flex-1 animate-pulse rounded bg-muted" />
        </div>
      )
    case 'cancelled':
      return <div className="p-3 text-xs text-muted-foreground">Cancelled. Press Refresh to run it again.</div>
    case 'error':
      return (
        <div role="alert" className="p-3 text-xs text-destructive" data-chart-error>
          {run.message}
        </div>
      )
  }

  const form = viz === 'table' || !model ? null : formFor(model, formOfViz(viz))
  if (!form) {
    return (
      <div className="flex h-full flex-col">
        {viz !== 'table' && <div className="px-3 pt-1 text-[11px] text-muted-foreground">This table can't be drawn as a chart, so it shows as a table.</div>}
        <div className="min-h-0 flex-1"><LogTableView table={run.table} elapsedMs={run.elapsedMs} /></div>
      </div>
    )
  }
  return <QueryChart table={run.table} form={form} options={options} colors={colors} hidden={hidden} className="pb-1" />
}
