import { useEffect, useMemo, useRef, useState } from 'react'
import { router } from '@inertiajs/react'
import { QueryEditor } from '@/components/QueryEditor'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  DASHBOARD_VIZES, newChartQuery, titleOfQuery, withSource,
  type ChartOptions, type DashboardChart, type DashboardViz,
} from '@/lib/dashboards/dashboard'
import { RunError, fetchRun, validateQuery, type QueryProblem } from '@/lib/logs/api'
import { EMPTY_BUILDER, builderOf, queryOf, type Builder } from '@/lib/logs/builder'
import { fieldInfos } from '@/lib/logs/columns'
import { cn } from '@/lib/utils'
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
 * a preview of its chart over the Dashboard's range as it changes. Saving
 * posts through Inertia, which reloads the Dashboard.
 */
export function ChartEditor({ chart, projects, orgSlug, dashboardId, ran, search, onClose }: {
  /** The chart edited, or null for a new one. */
  chart: DashboardChart | null
  projects: ProjectOption[]
  orgSlug: string
  dashboardId: number
  /** The times the Dashboard's charts ran over, which the preview runs over too. */
  ran: { from: Date; to: Date }
  /** The Dashboard's URL search, so it keeps its range after saving. */
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
        const { errors } = await validateQuery({ orgSlug, projectSlug: project.slug, query, signal: controller.signal })
        setProblems(errors)
        if (errors.length > 0) {
          const p = errors[0]
          setPreview({ status: 'error', message: `Line ${p.line}, column ${p.column}: ${p.message}` })
          return
        }
        const answer = await fetchRun({ orgSlug, projectSlug: project.slug, query, from: ran.from, to: ran.to, signal: controller.signal })
        if (answer.kind === 'table') setPreview({ status: 'done', table: answer.table, elapsedMs: answer.elapsedMs })
        else setPreview({ status: 'error', message: 'This Query answers Logs, not a table. End it with summarize, count or project to chart it.' })
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
      const { errors: mistakes } = await validateQuery({ orgSlug, projectSlug: project.slug, query })
      setProblems(mistakes)
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
          <div role="group" aria-label="Edit the Query in" className="flex w-fit overflow-hidden rounded-md border text-[11px]">
            {(['builder', 'query'] as const).map(m => (
              <button
                key={m}
                type="button"
                aria-pressed={mode === m}
                onClick={() => mode !== m && switchMode(m)}
                className={cn('h-6 border-l px-2 first:border-l-0', mode === m ? 'bg-accent font-medium text-accent-foreground' : 'text-muted-foreground hover:bg-muted')}
              >
                {m === 'builder' ? 'Builder' : 'Query'}
              </button>
            ))}
          </div>
          {notice && <p className="text-xs text-amber-600 dark:text-amber-400">{notice}</p>}
          {mode === 'builder' ? (
            <div className="rounded-md border">
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
            <div role="group" aria-label="Viz" className="flex overflow-hidden rounded-md border text-[11px]">
              {DASHBOARD_VIZES.map(v => (
                <button
                  key={v}
                  type="button"
                  aria-pressed={viz === v}
                  onClick={() => setViz(v)}
                  className={cn('h-7 border-l px-2 first:border-l-0', viz === v ? 'bg-accent font-medium text-accent-foreground' : 'text-muted-foreground hover:bg-muted')}
                >
                  {VIZ_LABELS[v]}
                </button>
              ))}
            </div>
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

        <div className="h-72 overflow-auto rounded-md border" data-chart-preview>
          <ChartBody run={preview} viz={viz} options={options} />
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>Cancel</Button>
          <Button onClick={save} disabled={saving}>{chart ? 'Save chart' : 'Add chart'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
