import { useEffect, useState } from 'react'
import { router } from '@inertiajs/react'
import { Copy, ExternalLink, MoreHorizontal, Pencil, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { fetchChartData } from '@/lib/dashboards/api'
import { explorerHref, type DashboardChart } from '@/lib/dashboards/dashboard'
import { isAbort, type FetchQueue } from '@/lib/dashboards/fetchQueue'
import { elapsedText } from '@/lib/logs/runLine'
import type { TimeRange } from '@/lib/timeRange'
import { cn } from '@/lib/utils'
import type { ProjectOption } from '../../Logs/components/BuilderPanel'
import { ChartBody, type ChartRun } from './ChartBody'

/** One run of every chart: the times it covers, and its number, which changes on Refresh. */
export interface DashboardRun {
  from: Date
  to: Date
  n: number
}

/**
 * A chart of the Dashboard: its title, its run over the Dashboard's range,
 * drawn as its viz, and a menu to open it in the explorer, edit, duplicate
 * or delete it. A failed run shows its error here. In Edit mode the title
 * is the handle that drags the card.
 */
export function ChartCard({ chart, project, orgSlug, run, range, queue, editing, onRunning, onEdit, onDuplicate, onDelete }: {
  chart: DashboardChart
  project: ProjectOption | undefined
  orgSlug: string
  run: DashboardRun
  /** The range shown, for Open in explorer. */
  range: TimeRange
  queue: FetchQueue
  editing: boolean
  onRunning: (id: number, running: boolean) => void
  onEdit: () => void
  onDuplicate: () => void
  onDelete: () => void
}) {
  const [answer, setAnswer] = useState<ChartRun>({ status: 'running' })

  useEffect(() => {
    let live = true
    const key = String(chart.id)
    setAnswer({ status: 'running' })
    onRunning(chart.id, true)
    queue.run(key, signal => fetchChartData({ orgSlug, dashboardId: chart.dashboard_id, chartId: chart.id, from: run.from, to: run.to, signal }))
      .then(({ table, elapsedMs }) => live && setAnswer({ status: 'done', table, elapsedMs }))
      .catch(error => live && setAnswer(isAbort(error) ? { status: 'cancelled' } : { status: 'error', message: error.message }))
      .finally(() => live && onRunning(chart.id, false))
    return () => {
      live = false
      queue.cancel(key)
      onRunning(chart.id, false)
    }
    // A run per Refresh or range, and again when the chart is saved.
  }, [run, chart.id, chart.updated_at])

  return (
    <div className="flex h-full flex-col overflow-hidden rounded-md border bg-card" data-chart-card={chart.title}>
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-1">
        <h2 className={cn('min-w-0 flex-1 truncate text-[13px] font-medium', editing && 'dashboard-chart-handle cursor-move')} title={chart.title}>
          {chart.title}
        </h2>
        {answer.status === 'done' && answer.elapsedMs !== null && (
          <span className="shrink-0 text-[11px] text-muted-foreground">{elapsedText(answer.elapsedMs)}</span>
        )}
        <span className="hidden shrink-0 text-[11px] text-muted-foreground sm:inline">{project?.name ?? 'Project gone'}</span>
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="h-6 w-6" aria-label={`Menu for ${chart.title}`} />}>
            <MoreHorizontal className="h-3.5 w-3.5" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-44">
            {project && (
              <DropdownMenuItem onClick={() => router.visit(explorerHref(orgSlug, project.slug, chart, range))}>
                <ExternalLink className="h-3.5 w-3.5" /> Open in explorer
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onClick={onEdit}><Pencil className="h-3.5 w-3.5" /> Edit</DropdownMenuItem>
            <DropdownMenuItem onClick={onDuplicate}><Copy className="h-3.5 w-3.5" /> Duplicate</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={onDelete} className="text-destructive"><Trash2 className="h-3.5 w-3.5" /> Delete</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <ChartBody run={answer} viz={chart.viz} options={chart.options} />
      </div>
    </div>
  )
}
