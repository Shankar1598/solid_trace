import { useCallback, useEffect, useMemo, useState } from 'react'
import { router, usePage } from '@inertiajs/react'
import { useContainerWidth } from 'react-grid-layout'
import 'react-grid-layout/css/styles.css'
import 'react-resizable/css/styles.css'
import { Check, ListPlus, Pencil, Plus, RefreshCw, Settings, Square } from 'lucide-react'
import { copyText } from '@/components/copyText'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { TimeRangePicker } from '@/components/TimeRangePicker'
import { AutoRefreshPicker, useAutoRefresh } from '@/components/AutoRefreshPicker'
import {
  AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { HoverTimeProvider } from '@/components/charts/hoverTime'
import { Button } from '@/components/ui/button'
import {
  chartGroups, pickedOfSearch, rangeOfSearch, searchOfRangeAndPicked, valuesWithDefaults,
  type AutoRefresh, type Dashboard, type DashboardChart, type DashboardSection, type FilterValues,
} from '@/lib/dashboards/dashboard'
import { createFetchQueue } from '@/lib/dashboards/fetchQueue'
import { readFolded, writeFolded } from '@/lib/dashboards/foldedSections'
import { browserStorage } from '@/lib/recentQueries'
import type { LinkKind } from '@/lib/logs/explorerState'
import { resolveRange, type TimeRange } from '@/lib/timeRange'
import type { SharedProps } from '@/types'
import type { ProjectOption } from '../Logs/components/BuilderPanel'
import { CopyLink } from '../Logs/components/CopyLink'
import { TagMenuProvider } from '../Logs/components/TagMenu'
import type { DashboardRun } from './components/ChartCard'
import { ChartEditor } from './components/ChartEditor'
import { ChartGrid } from './components/ChartGrid'
import { DashboardSettings } from './components/DashboardSettings'
import { FilterBar } from './components/FilterBar'
import { FiltersDialog } from './components/FiltersDialog'
import { DeleteSectionDialog, SectionDialog, SectionHeader } from './components/Sections'

/** Below this grid width (a phone) the cards stack in one column. */
const STACK_BELOW_PX = 640
const NO_COLUMNS = { shown: [], toggle: () => {} }

interface DashboardsShowProps {
  dashboard: Dashboard & { sections: DashboardSection[]; charts: DashboardChart[] }
  projects: ProjectOption[]
}

/**
 * A Dashboard: its charts in no Section on a grid, then each Section, a
 * header over its charts on a grid of their own. A viewer folds a Section to
 * its header, and the folds are kept in their browser only. Each chart runs
 * over the range in the URL (the
 * Dashboard's default range until another is picked) with the values of the
 * filters it names (their default values until others are picked), at most a
 * few at once. The values sit in the URL beside
 * the range; changing one runs again only the charts that name its filter.
 * Refresh runs them all again, as auto-refresh does on a relative range.
 * Edit lets the cards be dragged by their titles and resized from their
 * corners, inside their group; each move is saved.
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
  const [editor, setEditor] = useState<{ chart: DashboardChart | null; sectionId?: number | null } | null>(null)
  const [deleting, setDeleting] = useState<DashboardChart | null>(null)
  // The Section dialog: null renames none; section null adds one.
  const [sectionEditor, setSectionEditor] = useState<{ section: DashboardSection | null } | null>(null)
  const [deletingSection, setDeletingSection] = useState<DashboardSection | null>(null)
  const [folded, setFolded] = useState<ReadonlySet<number>>(() => readFolded(browserStorage(), dashboard.id))
  const [settings, setSettings] = useState(false)
  const [filtersOpen, setFiltersOpen] = useState(false)
  // The values picked, by name; [] is All. A filter not picked has its default values.
  const [picked, setPicked] = useState<FilterValues>(() => pickedOfSearch(window.location.search, dashboard.filters))
  // Only the values of the filters the Dashboard has now: a removed filter's go.
  const values = useMemo(() => valuesWithDefaults(dashboard.filters, picked), [dashboard.filters, picked])
  const search = searchOfRangeAndPicked(range, dashboard.filters, picked)

  const rerun = useCallback((r: TimeRange) => setRun(prev => ({ ...resolveRange(r, new Date()), n: prev.n + 1 })), [])
  const pickRange = (next: TimeRange) => {
    setRange(next)
    router.replace({ url: basePath + searchOfRangeAndPicked(next, dashboard.filters, picked), preserveState: true, preserveScroll: true })
    rerun(next)
  }
  // The charts that name the filter run again when their values change; the run stays.
  const pickValues = (name: string, next: string[]) => {
    const nextPicked = { ...picked, [name]: next }
    setPicked(nextPicked)
    router.replace({ url: basePath + searchOfRangeAndPicked(range, dashboard.filters, nextPicked), preserveState: true, preserveScroll: true })
  }

  // Auto-refresh runs a relative range again, counting from when the last run
  // finished; an absolute range would answer the same.
  useAutoRefresh(dashboard.auto_refresh, range.kind === 'relative' && running.size === 0, () => rerun(range))

  const onRunning = useCallback((id: number, on: boolean) => setRunning(prev => {
    if (prev.has(id) === on) return prev
    const next = new Set(prev)
    if (on) next.add(id)
    else next.delete(id)
    return next
  }), [])

  const toggleFold = (id: number) => setFolded(prev => {
    const next = new Set(prev)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    writeFolded(browserStorage(), dashboard.id, next)
    return next
  })

  const { width, containerRef, mounted } = useContainerWidth()
  // A phone shows the cards one under another and can't move them: the
  // stacked places are never saved.
  const stacked = width < STACK_BELOW_PX
  const arranging = editing && !stacked

  const copyLink = (kind: LinkKind) => {
    const shown: TimeRange = kind === 'shown' && range.kind === 'relative' ? { kind: 'absolute', from: run.from, to: run.to } : range
    copyText(window.location.origin + basePath + searchOfRangeAndPicked(shown, dashboard.filters, picked), 'Link')
  }

  const setAutoRefresh = (value: AutoRefresh) => router.patch(`${basePath}${search}`, { dashboard: { auto_refresh: value } }, { preserveScroll: true, preserveState: true })
  const duplicate = (chart: DashboardChart) => router.post(`${basePath}/charts/${chart.id}/duplicate${search}`, {}, { preserveScroll: true, preserveState: true })
  const remove = (chart: DashboardChart) => router.delete(`${basePath}/charts/${chart.id}${search}`, {
    preserveScroll: true, preserveState: true, onFinish: () => setDeleting(null),
  })
  const moveSection = (section: DashboardSection, to: number) =>
    router.patch(`${basePath}/sections/${section.id}${search}`, { section: { position: to } }, { preserveScroll: true, preserveState: true })

  const projectsById = useMemo(() => new Map(projects.map(p => [p.id, p])), [projects])
  const charts = dashboard.charts
  const sections = dashboard.sections
  // Memoized, so a grid's charts change only when the Dashboard is reloaded.
  const groups = useMemo(() => chartGroups(sections, charts), [sections, charts])
  const [unsectioned, ...sectioned] = groups
  const severalProjects = new Set(charts.map(c => c.project_id)).size > 1
  const grid = (groupCharts: DashboardChart[]) => (
    <ChartGrid
      charts={groupCharts}
      width={width}
      stacked={stacked}
      arranging={arranging}
      orgSlug={orgSlug}
      dashboardId={dashboard.id}
      projectsById={projectsById}
      showProject={severalProjects}
      run={run}
      range={range}
      filters={dashboard.filters}
      values={values}
      queue={queue}
      onRunning={onRunning}
      onEdit={chart => setEditor({ chart })}
      onDuplicate={duplicate}
      onDelete={setDeleting}
    />
  )

  return (
    <DashboardLayout>
      <PageHeader
        className="mb-3"
        breadcrumbs={[{ label: 'Dashboards', href: `/${orgSlug}/dashboards` }, { label: dashboard.name }]}
        title={dashboard.name}
        description={dashboard.description || undefined}
        actions={<>
          <TimeRangePicker range={range} onChange={pickRange} />
          {running.size > 0 ? (
            <Button variant="outline" onClick={() => queue.abortAll()}><Square className="h-3.5 w-3.5" /> Cancel</Button>
          ) : (
            <Button variant="outline" onClick={() => rerun(range)}><RefreshCw className="h-3.5 w-3.5" /> Refresh</Button>
          )}
          <AutoRefreshPicker value={dashboard.auto_refresh} onChange={setAutoRefresh} paused={range.kind === 'absolute' ? 'the range is fixed' : null} />
          <CopyLink onCopy={copyLink} />
          <Button variant="outline" onClick={() => setEditor({ chart: null })}><Plus className="h-3.5 w-3.5" /> Add chart</Button>
          <Button variant="outline" onClick={() => setSectionEditor({ section: null })}><ListPlus className="h-3.5 w-3.5" /> Add section</Button>
          {editing ? (
            <Button onClick={() => setEditing(false)}><Check className="h-3.5 w-3.5" /> Done</Button>
          ) : (
            <Button variant="outline" onClick={() => setEditing(true)} disabled={charts.length === 0 || stacked}><Pencil className="h-3.5 w-3.5" /> Edit layout</Button>
          )}
          <Button variant="ghost" size="icon" className="h-8 w-8" aria-label="Dashboard settings" onClick={() => setSettings(true)}>
            <Settings className="h-4 w-4" />
          </Button>
        </>}
      />
      <FilterBar
        filters={dashboard.filters} values={values} orgSlug={orgSlug} dashboardId={dashboard.id} shown={run}
        onChange={pickValues} onEdit={() => setFiltersOpen(true)}
      />
      <div className="flex flex-col gap-3">
        {arranging && <p className="text-xs text-muted-foreground">Drag a chart by its title, or resize it from its bottom-right corner. Each change is saved.</p>}

        <TagMenuProvider onFilter={null} onSummarize={null} columns={NO_COLUMNS}>
          {/* Hovering one time chart marks its time on the others. */}
          <HoverTimeProvider>
          <div ref={containerRef} className="flex flex-col gap-4">
            {charts.length === 0 && sections.length === 0 ? (
              <div className="border border-dashed p-10 text-center text-sm text-muted-foreground">
                No charts yet. Press Add chart, or chart a Query in the Logs explorer and press Add to dashboard beside the chart switch.
              </div>
            ) : mounted && (<>
              {unsectioned.charts.length > 0 && grid(unsectioned.charts)}
              {sectioned.map(({ section, charts: inSection }, i) => section && (
                <section key={section.id} className="flex flex-col gap-3" aria-label={section.title}>
                  <SectionHeader
                    section={section}
                    chartCount={inSection.length}
                    folded={folded.has(section.id)}
                    first={i === 0}
                    last={i === sectioned.length - 1}
                    onToggle={() => toggleFold(section.id)}
                    onAddChart={() => {
                      // The new chart must show, so a folded Section unfolds.
                      if (folded.has(section.id)) toggleFold(section.id)
                      setEditor({ chart: null, sectionId: section.id })
                    }}
                    onRename={() => setSectionEditor({ section })}
                    onMove={by => moveSection(section, i + by)}
                    onDelete={() => setDeletingSection(section)}
                  />
                  {folded.has(section.id) ? null : inSection.length > 0 ? grid(inSection) : (
                    <div className="flex items-center justify-center gap-3 border border-dashed p-4 text-xs text-muted-foreground">
                      No charts in this section.
                      <Button variant="outline" size="sm" onClick={() => setEditor({ chart: null, sectionId: section.id })}>
                        <Plus className="h-3.5 w-3.5" /> Add chart
                      </Button>
                    </div>
                  )}
                </section>
              ))}
            </>)}
          </div>
          </HoverTimeProvider>
        </TagMenuProvider>
      </div>

      {editor && (
        <ChartEditor
          chart={editor.chart}
          sectionId={editor.sectionId}
          projects={projects}
          sections={sections}
          orgSlug={orgSlug}
          dashboardId={dashboard.id}
          filters={dashboard.filters}
          ran={run}
          values={values}
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

      {sectionEditor && (
        <SectionDialog section={sectionEditor.section} orgSlug={orgSlug} dashboardId={dashboard.id} search={search} onClose={() => setSectionEditor(null)} />
      )}
      <DeleteSectionDialog
        section={deletingSection}
        chartCount={charts.filter(c => c.section_id === deletingSection?.id).length}
        orgSlug={orgSlug}
        dashboardId={dashboard.id}
        search={search}
        onClose={() => setDeletingSection(null)}
      />

      {settings && <DashboardSettings dashboard={dashboard} orgSlug={orgSlug} search={search} onClose={() => setSettings(false)} />}
      {filtersOpen && (
        <FiltersDialog dashboard={dashboard} projects={projects} orgSlug={orgSlug} search={search} onClose={() => setFiltersOpen(false)} />
      )}
    </DashboardLayout>
  )
}
