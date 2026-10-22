import { useEffect, useMemo, useRef, useState } from 'react'
import { router } from '@inertiajs/react'
import { QueryEditor } from '@/components/QueryEditor'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Segmented } from '@/components/ui/segmented'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  DASHBOARD_VIZES, newChartQuery, titleOfQuery, unknownFilterProblems, withSource,
  type ChartOptions, type DashboardChart, type DashboardFilter, type DashboardViz, type FilterValues,
} from '@/lib/dashboards/dashboard'
import { fetchChartPreview } from '@/lib/dashboards/api'
import { RunError, validateQuery, type QueryProblem } from '@/lib/logs/api'
import { EMPTY_BUILDER, builderOf, queryOf, type Builder } from '@/lib/logs/builder'
import { fieldInfos } from '@/lib/logs/columns'
import { BuilderPanel, type ProjectOption } from '../../Logs/components/BuilderPanel'
import { ChartBody, type ChartRun } from './ChartBody'

const VIZ_LABELS: Record<DashboardViz, string> = { line: 'Line', area: 'Area', bar: 'Bar', stat: 'Stat', table: 'Table' }
const NO_FIELDS = fieldInfos([])
const noValues = () => []
/** How long typing pauses before the preview runs again. */
const PREVIEW_DELAY_MS = 500

type Mode = 'builder' | 'query'

/**
 * Adds a chart, or edits one: its Project, its Query in the Builder or the
 * Query editor (checked through validate), its title, viz and options, with
 * a preview of its chart over the Dashboard's range and filter values as it
 * changes. A Query naming a filter the Dashboard lacks can't be saved.
 * Saving posts through Inertia, which reloads the Dashboard.
 */
export function ChartEditor({ chart, projects, orgSlug, dashboardId, filters, ran, values, search, onClose }: {
  /** The chart edited, or null for a new one. */
  chart: DashboardChart | null
  projects: ProjectOption[]
  orgSlug: string
  dashboardId: number
  /** The Dashboard's filters, which the Builder offers and the Query may name. */
  filters: DashboardFilter[]
  /** The times the Dashboard's charts ran over, which the preview runs over too. */
  ran: { from: Date; to: Date }
  /** The filter bar's values, which the preview runs with. */
  values: FilterValues
  /** The Dashboard's URL search, so it keeps its range and filter values after saving. */
  search: string
  onClose: () => void
}) {
  const [projectId, setProjectId] = useState(chart?.project_id ?? projects[0]?.id)
  const project = projects.find(p => p.id === projectId) ?? projects[0]
  const [query, setQuery] = useState(() => chart?.query ?? newChartQuery(project?.slug ?? ''))
  const [mode, setMode] = useState<Mode>('builder')
  const [builder, setBuilder] = useState<Builder | null>(null)
  const [title, setTitle] = useState(chart?.title ?? '')
  const [viz, setViz] = useState<DashboardViz>(chart?.viz ?? 'line')
  const [options, setOptions] = useState<ChartOptions>(chart?.options ?? {})
  const [problems, setProblems] = useState<QueryProblem[]>([])
  // The Query names a filter the Dashboard lacks, which keeps Save off.
  const [namesUnknown, setNamesUnknown] = useState(false)
  const [preview, setPreview] = useState<ChartRun>({ status: 'running' })
  const [notice, setNotice] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  // Cmd/Ctrl+Enter in the Query runs the preview again.
  const [again, setAgain] = useState(0)
  const [errors, setErrors] = useState<Record<string, string>>({})

  // The Builder reads its chips from validate: a Query it can't show opens in Query mode.
  useEffect(() => {
    if (!project) return
    const controller = new AbortController()
    validateQuery({ orgSlug, projectSlug: project.slug, query, signal: controller.signal })
      .then(({ chips }) => {
        if (chips) setBuilder(builderOf(chips))
        else setMode('query')
      })
      .catch(() => { if (!controller.signal.aborted) setMode('query') })
    return () => controller.abort()
    // Once, for the Query the editor opened with.
  }, [])

  // The Query's problems: validate's mistakes, then each name that isn't one of the Dashboard's filters.
  const check = async (signal?: AbortSignal) => {
    const { errors, parameters } = await validateQuery({ orgSlug, projectSlug: project.slug, query, signal })
    const unknown = unknownFilterProblems(query, parameters, filters)
    setProblems([...errors, ...unknown])
    setNamesUnknown(unknown.length > 0)
    return [...errors, ...unknown]
  }

  // The preview: the Query checked, then run over the Dashboard's range, a moment after the last change.
  const previewRun = useRef<AbortController | null>(null)
  useEffect(() => {
    if (!project) return
    const controller = new AbortController()
    previewRun.current?.abort()
    previewRun.current = controller
    setPreview({ status: 'running' })
    const timer = setTimeout(async () => {
      try {
        const errors = await check(controller.signal)
        if (errors.length > 0) {
          const p = errors[0]
          setPreview({ status: 'error', message: `Line ${p.line}, column ${p.column}: ${p.message}` })
          return
        }
        // Run as a chart's data is, with its timeout: a run abandoned for the next edit still holds a Console thread.
        const answer = await fetchChartPreview({
          orgSlug, dashboardId, projectId: project.id, query, from: ran.from, to: ran.to, parameters: values, signal: controller.signal,
        })
        setPreview({ status: 'done', table: answer.table, elapsedMs: answer.elapsedMs })
      } catch (error) {
        if (controller.signal.aborted) return
        setPreview({ status: 'error', message: error instanceof RunError ? error.message : 'The preview failed. Try again.' })
      }
    }, PREVIEW_DELAY_MS)
    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [query, project?.slug, ran, again])

  const pickProject = (id: number) => {
    const next = projects.find(p => p.id === id)
    if (!next) return
    setProjectId(id)
    setQuery(builder && mode === 'builder' ? queryOf(builder, next.slug) : withSource(query, next.slug))
  }

  const changeBuilder = (next: Builder) => {
    setBuilder(next)
    setQuery(queryOf(next, project.slug))
  }

  const switchMode = (next: Mode) => {
    setNotice(null)
    if (next === 'query') {
      setMode('query')
      return
    }
    validateQuery({ orgSlug, projectSlug: project.slug, query })
      .then(({ chips }) => {
        if (!chips) {
          setNotice("The Builder can't show this Query. Keep editing it here.")
          return
        }
        setBuilder(builderOf(chips))
        setMode('builder')
      })
      .catch(error => setNotice(error.message))
  }

  const save = async () => {
    setSaving(true)
    setErrors({})
    try {
      const mistakes = await check()
      if (mistakes.length > 0) {
        setSaving(false)
        setMode('query')
        return
      }
    } catch (error) {
      setErrors({ query: error instanceof Error ? error.message : 'The Query could not be checked.' })
      setSaving(false)
      return
    }
    // stacked is always sent, so the options are never an empty hash, which Rails would drop.
    const saved: Record<string, string | number | boolean> = { stacked: options.stacked !== false }
    if (options.unit) saved.unit = options.unit
    if (options.decimals !== undefined && !Number.isNaN(options.decimals)) saved.decimals = options.decimals
    const data = { chart: { project_id: project.id, title: title.trim() || titleOfQuery(query), query, viz, options: saved } }
    // The page keeps its range and runs, so only the chart saved runs again.
    const visit = {
      preserveScroll: true,
      preserveState: true,
      onSuccess: onClose,
      onError: (e: Record<string, string>) => setErrors(e),
      onFinish: () => setSaving(false),
    }
    if (chart) router.patch(`/${orgSlug}/dashboards/${dashboardId}/charts/${chart.id}${search}`, data, visit)
    else router.post(`/${orgSlug}/dashboards/${dashboardId}/charts${search}`, data, visit)
  }

  const projectItems = useMemo(() => projects.map(p => ({ value: String(p.id), label: p.name })), [projects])
  if (!project) {
    return (
      <Dialog open onOpenChange={open => !open && onClose()}>
        <DialogContent>
          <DialogTitle>Add a chart</DialogTitle>
          <p className="text-sm text-muted-foreground">Charts read a Project's Logs. Create a Project first.</p>
        </DialogContent>
      </Dialog>
    )
  }

  return (
    <Dialog open onOpenChange={open => !open && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{chart ? 'Edit chart' : 'Add a chart'}</DialogTitle>
        </DialogHeader>

        <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
          <div className="space-y-1">
            <Label htmlFor="chart-title">Title</Label>
            <Input id="chart-title" value={title} placeholder={titleOfQuery(query)} onChange={e => setTitle(e.target.value)} />
            {errors.title && <p className="text-xs text-destructive">Title {errors.title}</p>}
          </div>
          <div className="space-y-1">
            <Label>Project</Label>
            <Select value={String(project.id)} onValueChange={value => value && pickProject(Number(value))} items={projectItems}>
              <SelectTrigger aria-label="Project" className="min-w-44">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {projectItems.map(p => <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>)}
              </SelectContent>
            </Select>
            {errors.project && <p className="text-xs text-destructive">Project {errors.project}</p>}
          </div>
        </div>

        <div className="space-y-2">
          <Segmented
            label="Edit the Query in"
            size="sm"
            value={mode}
            options={[{ value: 'builder', label: 'Builder' }, { value: 'query', label: 'Query' }]}
            onChange={switchMode}
          />
          {notice && <p className="text-xs text-warning">{notice}</p>}
          {mode === 'builder' ? (
            <div className="border">
              {builder ? (
                <BuilderPanel
                  builder={builder}
                  project={project}
                  projects={projects}
                  onProject={slug => {
                    const next = projects.find(p => p.slug === slug)
                    if (next) pickProject(next.id)
                  }}
                  fields={NO_FIELDS}
                  valuesOf={noValues}
                  filters={filters}
                  onChange={changeBuilder}
                />
              ) : (
                <div className="p-3 text-xs text-muted-foreground">Reading the Query…</div>
              )}
            </div>
          ) : (
            <div>
              <QueryEditor
                value={query}
                onChange={setQuery}
                onRun={() => setAgain(n => n + 1)}
                language="query"
                label="Query"
                problems={problems}
                completions={{ fields: NO_FIELDS, valuesOf: noValues, source: queryOf(EMPTY_BUILDER, project.slug) }}
                maxLines={10}
                autoFocus
              />
              {problems.map((p, i) => (
                <div key={i} className="mt-0.5 font-mono text-[11px] text-destructive">Line {p.line}, column {p.column}: {p.message}</div>
              ))}
            </div>
          )}
          {errors.query && <p className="text-xs text-destructive">Query {errors.query}</p>}
        </div>

        <div className="flex flex-wrap items-end gap-4">
          <div className="space-y-1">
            <Label>Viz</Label>
            <Segmented label="Viz" value={viz} options={DASHBOARD_VIZES.map(v => ({ value: v, label: VIZ_LABELS[v] }))} onChange={setViz} />
          </div>
          <label className="flex items-center gap-2 text-xs">
            <Checkbox
              checked={options.stacked !== false}
              onCheckedChange={checked => setOptions(o => ({ ...o, stacked: checked === true ? undefined : false }))}
            />
            Stack where values add up
          </label>
          <div className="space-y-1">
            <Label htmlFor="chart-unit">Unit</Label>
            <Input id="chart-unit" className="h-7 w-24" value={options.unit ?? ''} placeholder="ms" maxLength={16}
              onChange={e => setOptions(o => ({ ...o, unit: e.target.value || undefined }))} />
          </div>
          <div className="space-y-1">
            <Label htmlFor="chart-decimals">Decimals</Label>
            <Input id="chart-decimals" className="h-7 w-20" type="number" min={0} max={6} value={options.decimals ?? ''} placeholder="auto"
              onChange={e => setOptions(o => ({ ...o, decimals: e.target.value === '' ? undefined : Number(e.target.value) }))} />
          </div>
          {errors.options && <p className="text-xs text-destructive">Options {errors.options}</p>}
        </div>

        <div className="h-72 overflow-auto border" data-chart-preview>
          <ChartBody run={preview} viz={viz} options={options} />
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>Cancel</Button>
          <Button onClick={save} disabled={saving || namesUnknown}>{chart ? 'Save chart' : 'Add chart'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
