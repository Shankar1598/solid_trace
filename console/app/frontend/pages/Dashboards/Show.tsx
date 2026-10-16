import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { router, usePage } from '@inertiajs/react'
import ReactGridLayout, { useContainerWidth, type Layout, type LayoutItem } from 'react-grid-layout'
import 'react-grid-layout/css/styles.css'
import 'react-resizable/css/styles.css'
import { toast } from 'sonner'
import { Check, Pencil, Plus, RefreshCw, Settings, Square } from 'lucide-react'
import { copyText } from '@/components/copyText'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { TimeRangePicker } from '@/components/TimeRangePicker'
import {
  AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { saveLayout } from '@/lib/dashboards/api'
import {
  AUTO_REFRESH_MS, GRID_COLUMNS, movedPlaces, rangeOfSearch, searchOfRange,
  type AutoRefresh, type Dashboard, type DashboardChart, type Place,
} from '@/lib/dashboards/dashboard'
import { createFetchQueue } from '@/lib/dashboards/fetchQueue'
import type { LinkKind } from '@/lib/logs/explorerState'
import { resolveRange, type TimeRange } from '@/lib/timeRange'
import type { SharedProps } from '@/types'
import type { ProjectOption } from '../Logs/components/BuilderPanel'
import { CopyLink } from '../Logs/components/CopyLink'
import { TagMenuProvider } from '../Logs/components/TagMenu'
import { ChartCard, type DashboardRun } from './components/ChartCard'
import { ChartEditor } from './components/ChartEditor'
import { DashboardSettings } from './components/DashboardSettings'

/** A grid row's height and the gap between cards, in px. */
const ROW_PX = 64
const GAP_PX = 12
const AUTO_REFRESH_LABELS: Record<AutoRefresh, string> = { off: 'Auto-refresh off', '1m': 'Every minute', '5m': 'Every 5 minutes' }
const NO_COLUMNS = { shown: [], toggle: () => {} }

interface DashboardsShowProps {
  dashboard: Dashboard & { charts: DashboardChart[] }
  projects: ProjectOption[]
}

const placesOf = (charts: DashboardChart[]): Place[] => charts.map(({ id, x, y, w, h }) => ({ id, x, y, w, h }))
const layoutOf = (charts: DashboardChart[]): LayoutItem[] => charts.map(c => ({ i: String(c.id), x: c.x, y: c.y, w: c.w, h: c.h, minW: 2, minH: 2 }))

/**
 * A Dashboard: its charts on a grid, each run over the range in the URL (the
 * Dashboard's default range until another is picked), at most a few at once.
 * Refresh runs them again, as auto-refresh does on a relative range. Edit
 * lets the cards be dragged by their titles and resized from their corners;
 * each move is saved.
 */
export default function DashboardsShow({ dashboard, projects }: DashboardsShowProps) {
  const orgSlug = usePage<SharedProps>().props.current_org?.slug ?? ''
  const basePath = `/${orgSlug}/dashboards/${dashboard.id}`
  const [range, setRange] = useState<TimeRange>(() => rangeOfSearch(window.location.search, dashboard.default_range))
  const [run, setRun] = useState<DashboardRun>(() => ({ ...resolveRange(range, new Date()), n: 0 }))
  const queue = useMemo(() => createFetchQueue(), [])
  useEffect(() => () => queue.abortAll(), [queue])
  const [running, setRunning] = useState<ReadonlySet<number>>(new Set())
  const [editing, setEditing] = useState(false)
  const [editor, setEditor] = useState<{ chart: DashboardChart | null } | null>(null)
  const [deleting, setDeleting] = useState<DashboardChart | null>(null)
  const [settings, setSettings] = useState(false)
  const search = searchOfRange(range)

  const rerun = useCallback((r: TimeRange) => setRun(prev => ({ ...resolveRange(r, new Date()), n: prev.n + 1 })), [])
  const pickRange = (next: TimeRange) => {
    setRange(next)
    router.replace({ url: basePath + searchOfRange(next), preserveState: true, preserveScroll: true })
    rerun(next)
  }

  // Auto-refresh runs a relative range again; an absolute one would answer the same.
  useEffect(() => {
    const every = AUTO_REFRESH_MS[dashboard.auto_refresh]
    if (!every || range.kind !== 'relative') return
    const timer = setInterval(() => rerun(range), every)
    return () => clearInterval(timer)
  }, [dashboard.auto_refresh, range, rerun])

  const onRunning = useCallback((id: number, on: boolean) => setRunning(prev => {
    if (prev.has(id) === on) return prev
    const next = new Set(prev)
    if (on) next.add(id)
    else next.delete(id)
    return next
  }), [])

  // The grid's places, and the ones last saved, so each move saves only what changed.
  const [layout, setLayout] = useState<LayoutItem[]>(() => layoutOf(dashboard.charts))
  const saved = useRef<Place[]>(placesOf(dashboard.charts))
  useEffect(() => {
    setLayout(layoutOf(dashboard.charts))
    saved.current = placesOf(dashboard.charts)
  }, [dashboard.charts])
  const persist = (next: Layout) => {
    const moved = movedPlaces(saved.current, [...next])
    if (moved.length === 0) return
    const before = saved.current
    saved.current = saved.current.map(p => moved.find(m => m.id === p.id) ?? p)
    saveLayout(orgSlug, dashboard.id, moved).catch(error => {
      saved.current = before
      toast.error(`The layout wasn't saved: ${error.message}`)
    })
  }
  const { width, containerRef, mounted } = useContainerWidth()

  const copyLink = (kind: LinkKind) => {
    const shown: TimeRange = kind === 'shown' && range.kind === 'relative' ? { kind: 'absolute', from: run.from, to: run.to } : range
    copyText(window.location.origin + basePath + searchOfRange(shown), 'Link')
  }

  const setAutoRefresh = (value: AutoRefresh) => router.patch(`${basePath}${search}`, { dashboard: { auto_refresh: value } }, { preserveScroll: true, preserveState: true })
  const duplicate = (chart: DashboardChart) => router.post(`${basePath}/charts/${chart.id}/duplicate${search}`, {}, { preserveScroll: true, preserveState: true })
  const remove = (chart: DashboardChart) => router.delete(`${basePath}/charts/${chart.id}${search}`, {
    preserveScroll: true, preserveState: true, onFinish: () => setDeleting(null),
  })

  const projectsById = useMemo(() => new Map(projects.map(p => [p.id, p])), [projects])
  const charts = dashboard.charts
  const autoRefreshItems = (Object.keys(AUTO_REFRESH_LABELS) as AutoRefresh[]).map(value => ({ value, label: AUTO_REFRESH_LABELS[value] }))

  return (
    <DashboardLayout>
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <div className="mr-auto min-w-0">
            <h1 className="truncate text-xl font-semibold">{dashboard.name}</h1>
            {dashboard.description && <p className="truncate text-xs text-muted-foreground">{dashboard.description}</p>}
          </div>
          <TimeRangePicker range={range} onChange={pickRange} />
          {running.size > 0 ? (
            <Button variant="outline" size="sm" onClick={() => queue.abortAll()}><Square className="h-3.5 w-3.5" /> Cancel</Button>
          ) : (
            <Button variant="outline" size="sm" onClick={() => rerun(range)}><RefreshCw className="h-3.5 w-3.5" /> Refresh</Button>
          )}
          <Select value={dashboard.auto_refresh} onValueChange={value => value && setAutoRefresh(value as AutoRefresh)} items={autoRefreshItems}>
            <SelectTrigger size="sm" aria-label="Auto-refresh" className="h-8">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {autoRefreshItems.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
            </SelectContent>
          </Select>
          <CopyLink onCopy={copyLink} />
          <Button variant="outline" size="sm" onClick={() => setEditor({ chart: null })}><Plus className="h-3.5 w-3.5" /> Add chart</Button>
          {editing ? (
            <Button size="sm" onClick={() => setEditing(false)}><Check className="h-3.5 w-3.5" /> Done</Button>
          ) : (
            <Button variant="outline" size="sm" onClick={() => setEditing(true)} disabled={charts.length === 0}><Pencil className="h-3.5 w-3.5" /> Edit layout</Button>
          )}
          <Button variant="ghost" size="icon" className="h-8 w-8" aria-label="Dashboard settings" onClick={() => setSettings(true)}>
            <Settings className="h-4 w-4" />
          </Button>
        </div>
        {editing && <p className="text-xs text-muted-foreground">Drag a chart by its title, or resize it from its bottom-right corner. Each change is saved.</p>}

        <TagMenuProvider onFilter={null} onSummarize={null} columns={NO_COLUMNS}>
          <div ref={containerRef}>
            {charts.length === 0 ? (
              <div className="rounded-md border border-dashed p-10 text-center text-sm text-muted-foreground">
                No charts yet. Press Add chart, or chart a Query in the Logs explorer and press Add to dashboard beside the chart switch.
              </div>
            ) : mounted && (
              <ReactGridLayout
                layout={layout}
                width={width}
                gridConfig={{ cols: GRID_COLUMNS, rowHeight: ROW_PX, margin: [GAP_PX, GAP_PX], containerPadding: [0, 0] }}
                dragConfig={{ enabled: editing, handle: '.dashboard-chart-handle' }}
                resizeConfig={{ enabled: editing }}
                onLayoutChange={next => setLayout([...next])}
                onDragStop={persist}
                onResizeStop={persist}
              >
                {charts.map(chart => (
                  <div key={String(chart.id)}>
                    <ChartCard
                      chart={chart}
                      project={projectsById.get(chart.project_id)}
                      orgSlug={orgSlug}
                      run={run}
                      range={range}
                      queue={queue}
                      editing={editing}
                      onRunning={onRunning}
                      onEdit={() => setEditor({ chart })}
                      onDuplicate={() => duplicate(chart)}
                      onDelete={() => setDeleting(chart)}
                    />
                  </div>
                ))}
              </ReactGridLayout>
            )}
          </div>
        </TagMenuProvider>
      </div>

      {editor && (
        <ChartEditor
          chart={editor.chart}
          projects={projects}
          orgSlug={orgSlug}
          dashboardId={dashboard.id}
          ran={run}
          search={search}
          onClose={() => setEditor(null)}
        />
      )}

      <AlertDialog open={deleting !== null} onOpenChange={open => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete "{deleting?.title}"?</AlertDialogTitle>
            <AlertDialogDescription>The chart is removed from this Dashboard for everyone. Its Query isn't kept.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button variant="destructive" onClick={() => deleting && remove(deleting)}>Delete chart</Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {settings && <DashboardSettings dashboard={dashboard} orgSlug={orgSlug} search={search} onClose={() => setSettings(false)} />}
    </DashboardLayout>
  )
}
