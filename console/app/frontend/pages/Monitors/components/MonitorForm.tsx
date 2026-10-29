import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, router } from '@inertiajs/react'
import { QueryChart } from '@/components/charts/QueryChart'
import { FormSection } from '@/components/FormSection'
import { QueryEditor } from '@/components/QueryEditor'
import { Button, buttonVariants } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Segmented } from '@/components/ui/segmented'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ChartSkeleton } from '@/components/ui/skeleton'
import { valueText } from '@/lib/charts/axis'
import { fetchPreview } from '@/lib/dashboards/api'
import { withSource } from '@/lib/dashboards/dashboard'
import { providerName } from '@/lib/integrations'
import { RunError, validateQuery, type QueryProblem } from '@/lib/logs/api'
import { EMPTY_BUILDER, builderOf, queryOf, type Builder } from '@/lib/logs/builder'
import { fieldInfos } from '@/lib/logs/columns'
import type { LogTable } from '@/lib/logs/table'
import { fetchGroupsPreview, type PreviewGroup } from '@/lib/monitors/api'
import {
  BASELINES, CHANGE_OPERATORS, CHANGE_WORDS, MAX_INTERVAL_MINUTES, MAX_RANGE_MINUTES, OPERATORS, baselineOffsetMinutes, baselineRangeText, changeText,
  groupLabel, groupsNow, minimumText, previewCaption, withBaselineSeries, withRangeBins, withoutTimeBins, type ChangeOperator, type IntegrationOption,
  type MonitorBaseline, type MonitorCompare, type MonitorKind,
  type MonitorSettings, type ValueOperator,
} from '@/lib/monitors/monitor'
import { cn } from '@/lib/utils'
import { BuilderPanel, type ProjectOption } from '../../Logs/components/BuilderPanel'

const NO_FIELDS = fieldInfos([])
const noValues = () => []
/** How long typing pauses before the preview runs again. */
const PREVIEW_DELAY_MS = 500
/** The preview chart's span. */
const CHART_HOURS = 24
/** A run's range ends this long before it, as LogMonitor::PROCESSING_LAG. */
const PROCESSING_LAG_MS = 60_000
/** Groups listed under the chart; the rest are counted. */
const GROUPS_SHOWN = 20

type Mode = 'builder' | 'query'
type Run = { status: 'running' } | { status: 'done'; table: LogTable } | { status: 'error'; message: string }
type GroupsRun = { status: 'running' } | { status: 'done'; groups: PreviewGroup[] } | { status: 'error'; message: string }

/**
 * Makes or edits a Monitor: its name, Project and kind, its Query in the
 * Builder or the Query editor (checked through validate), its schedule and
 * rule, and its Integrations. A Threshold monitor previews its Query over the
 * last 24 hours, binned by its range, with the threshold drawn, and lists the
 * groups a run would fire now. Comparing the change, the chart has the Query
 * now and over the Baseline's 24 hours, moved onto the same axis, and the
 * server lists the groups that would fire, compared as a run compares them. Saving posts through Inertia; the server's
 * errors show beside their fields.
 */
export function MonitorForm({ monitor, monitorId, projects, integrations, orgSlug }: {
  monitor: MonitorSettings
  /** The Monitor edited, or undefined for a new one. */
  monitorId?: number
  projects: ProjectOption[]
  integrations: IntegrationOption[]
  orgSlug: string
}) {
  const [name, setName] = useState(monitor.name)
  const [kind, setKind] = useState<MonitorKind>(monitor.kind)
  const [projectId, setProjectId] = useState(monitor.project_id ?? projects[0]?.id)
  const project = projects.find(p => p.id === projectId) ?? projects[0]
  const [query, setQuery] = useState(monitor.query)
  const [mode, setMode] = useState<Mode>('builder')
  const [builder, setBuilder] = useState<Builder | null>(null)
  const [intervalText, setIntervalText] = useState(String(monitor.interval_minutes))
  const [range, setRange] = useState(String(monitor.range_minutes))
  const [compare, setCompare] = useState<MonitorCompare>(monitor.compare ?? 'value')
  const [baseline, setBaseline] = useState<MonitorBaseline>(monitor.baseline ?? 'previous')
  // Each comparison keeps its own operator, so switching back and forth keeps what was picked.
  const [operator, setOperator] = useState<ValueOperator>(
    OPERATORS.find(o => o === monitor.operator) ?? '>')
  const [direction, setDirection] = useState<ChangeOperator>(
    CHANGE_OPERATORS.find(o => o === monitor.operator) ?? 'rises')
  const [threshold, setThreshold] = useState(monitor.threshold === null ? '' : String(monitor.threshold))
  const [minimum, setMinimum] = useState(monitor.minimum == null ? '' : String(monitor.minimum))
  const [byGroup, setByGroup] = useState(monitor.notify_by_group)
  const [noData, setNoData] = useState(monitor.alert_on_no_data)
  const [integrationIds, setIntegrationIds] = useState<number[]>(monitor.integration_ids)
  const [problems, setProblems] = useState<QueryProblem[]>([])
  const [notice, setNotice] = useState<string | null>(null)
  const [errors, setErrors] = useState<Record<string, unknown>>({})
  const [saving, setSaving] = useState(false)
  const [chart, setChart] = useState<Run>({ status: 'running' })
  const [now, setNow] = useState<Run>({ status: 'running' })
  const [changed, setChanged] = useState<GroupsRun>({ status: 'running' })
  // Cmd/Ctrl+Enter in the Query, or Refresh, runs the preview again.
  const [again, setAgain] = useState(0)

  const rangeMinutes = wholeIn(range, 1, MAX_RANGE_MINUTES)
  const thresholdValue = threshold.trim() === '' || Number.isNaN(Number(threshold)) ? null : Number(threshold)

  // The Builder reads its chips from validate: a Query it can't show opens in Query mode. A new
  // Monitor's Query from the explorer loses its time bins, or, with no Summarize, is a Match monitor's.
  useEffect(() => {
    if (!project) return
    const controller = new AbortController()
    validateQuery({ orgSlug, projectSlug: project.slug, query, signal: controller.signal })
      .then(({ chips }) => {
        if (!chips) return setMode('query')
        let read = builderOf(chips)
        if (monitorId === undefined) {
          if (!read.summarize || read.summarize.aggregates.length === 0) {
            setKind('match')
          } else {
            const unbinned = withoutTimeBins(read)
            if (unbinned !== read) {
              read = unbinned
              setQuery(queryOf(read, project.slug))
              setNotice('The time bins were taken out: a Threshold monitor compares one number per group over its whole range.')
            }
          }
        }
        setBuilder(read)
      })
      .catch(() => { if (!controller.signal.aborted) setMode('query') })
    return () => controller.abort()
    // Once, for the Query the form opened with.
  }, [])

  // A Threshold monitor's preview, a moment after the last change: the Query checked, then run
  // over the range a run now reads, then binned by that range over the last 24 hours. Comparing
  // the change, the server lists the groups (below), and the chart runs again over the
  // Baseline's 24 hours.
  const previewRun = useRef<AbortController | null>(null)
  useEffect(() => {
    if (!project || kind !== 'threshold') return
    const withBaseline = compare === 'change'
    const controller = new AbortController()
    previewRun.current?.abort()
    previewRun.current = controller
    setChart({ status: 'running' })
    setNow({ status: 'running' })
    const fail = (error: unknown) => (error instanceof RunError ? error.message : 'The preview failed. Try again.')
    const timer = setTimeout(async () => {
      const url = `/${orgSlug}/monitors/preview`
      const signal = controller.signal
      try {
        const { errors, chips } = await validateQuery({ orgSlug, projectSlug: project.slug, query, signal })
        setProblems(errors)
        if (errors.length > 0) {
          const p = errors[0]
          const message = `Line ${p.line}, column ${p.column}: ${p.message}`
          setChart({ status: 'error', message })
          setNow({ status: 'error', message })
          return
        }
        const to = new Date(Date.now() - PROCESSING_LAG_MS)
        if (!withBaseline) {
          try {
            const answer = await fetchPreview(url, { projectId: project.id, query, from: new Date(to.getTime() - rangeMinutes * 60_000), to, signal })
            setNow({ status: 'done', table: answer.table })
          } catch (error) {
            if (signal.aborted) return
            setNow({ status: 'error', message: fail(error) })
          }
        }
        const binned = chips ? withRangeBins(builderOf(chips), rangeMinutes) : null
        if (!binned) {
          setChart({ status: 'error', message: 'The chart needs a Query the Builder can show, ending in a summarize. The groups below still show what a run would find now.' })
          return
        }
        const chartOver = (end: Date) => fetchPreview(url, {
          projectId: project.id, query: queryOf(binned, project.slug), from: new Date(end.getTime() - CHART_HOURS * 3_600_000), to: end, signal,
        })
        const answer = await chartOver(to)
        if (!withBaseline) return setChart({ status: 'done', table: answer.table })
        const offsetMs = baselineOffsetMinutes(baseline, rangeMinutes) * 60_000
        const before = await chartOver(new Date(to.getTime() - offsetMs))
        setChart({ status: 'done', table: withBaselineSeries(answer.table, before.table, offsetMs) })
      } catch (error) {
        if (signal.aborted) return
        setChart({ status: 'error', message: fail(error) })
        setNow(n => (n.status === 'running' ? { status: 'error', message: fail(error) } : n))
      }
    }, PREVIEW_DELAY_MS)
    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [query, project?.slug, rangeMinutes, kind, compare, baseline, again])

  // Comparing the change, the groups that would fire now, a moment after the last change: read
  // over both ranges and compared on the server, as a run reads and compares them.
  const comparesChange = compare === 'change'
  useEffect(() => {
    if (!project || kind !== 'threshold' || !comparesChange) return
    const controller = new AbortController()
    setChanged({ status: 'running' })
    const timer = setTimeout(async () => {
      try {
        const answer = await fetchGroupsPreview(orgSlug, {
          project_id: project.id, query, range_minutes: rangeMinutes, compare, baseline, operator: direction,
          threshold: thresholdValue, minimum: minimum.trim(), notify_by_group: byGroup, alert_on_no_data: noData,
        }, controller.signal)
        setChanged({ status: 'done', groups: answer.groups })
      } catch (error) {
        if (controller.signal.aborted) return
        setChanged({ status: 'error', message: error instanceof RunError ? error.message : 'The preview failed. Try again.' })
      }
    }, PREVIEW_DELAY_MS)
    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [query, project?.id, rangeMinutes, kind, comparesChange, baseline, direction, thresholdValue, minimum, byGroup, noData, again])

  const found = useMemo(
    () => (now.status === 'done'
      ? groupsNow(now.table, { operator, threshold: thresholdValue, notify_by_group: byGroup, alert_on_no_data: noData })
      : null),
    [now, operator, thresholdValue, byGroup, noData],
  )

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

  const toggleIntegration = (id: number, on: boolean) =>
    setIntegrationIds(ids => (on ? [...ids.filter(i => i !== id), id] : ids.filter(i => i !== id)))

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
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
    const data = {
      monitor: {
        name: name.trim(), kind, project_id: project.id, query,
        interval_minutes: intervalText, range_minutes: range, compare, baseline: comparesChange ? baseline : null,
        operator: comparesChange ? direction : operator, threshold, minimum: comparesChange ? minimum.trim() : '',
        notify_by_group: byGroup, alert_on_no_data: noData, integration_ids: integrationIds,
      },
    }
    const visit = {
      preserveScroll: true,
      preserveState: true,
      onError: (e: Record<string, unknown>) => setErrors(e),
      onFinish: () => setSaving(false),
    }
    if (monitorId === undefined) router.post(`/${orgSlug}/monitors`, data, visit)
    else router.patch(`/${orgSlug}/monitors/${monitorId}`, data, visit)
  }

  const projectItems = useMemo(() => projects.map(p => ({ value: String(p.id), label: p.name })), [projects])
  if (!project) {
    return (
      <p className="text-sm text-muted-foreground">
        Monitors read a Project's Logs. <Link href={`/${orgSlug}/projects/new`} className="underline">Create a Project</Link> first.
      </p>
    )
  }

  const isThreshold = kind === 'threshold'
  const groups: PreviewGroup[] | undefined = comparesChange
    ? (changed.status === 'done' ? changed.groups : undefined)
    : found?.groups.map(g => ({ ...g, baseline: null }))
  const firing = groups?.filter(g => g.firing) ?? []
  const listed = [...firing, ...(groups?.filter(g => !g.firing) ?? [])]
  const groupsRun: Run | GroupsRun = comparesChange ? changed : now
  const minimumValue = minimum.trim() !== '' && Number(minimum) > 0 ? Number(minimum) : null
  // Comparing the value it follows the range; comparing the change it leads the rule's sentence.
  const compareField = (
    <div className="space-y-1">
      <Label>Compare</Label>
      <Segmented
        label="Compare"
        value={compare}
        options={[{ value: 'value', label: 'The value' }, { value: 'change', label: 'The change' }]}
        onChange={setCompare}
      />
      <FieldError name="Compare" error={errors.compare} />
    </div>
  )

  return (
    <form onSubmit={save} className="max-w-4xl space-y-6">
      <div className="space-y-4">
        <div className="grid gap-4 sm:grid-cols-[1fr_auto]">
          <div className="space-y-1">
            <Label htmlFor="monitor-name">Name</Label>
            <Input id="monitor-name" value={name} onChange={e => setName(e.target.value)} placeholder="Checkout errors" />
            <FieldError name="Name" error={errors.name} />
          </div>
          <div className="space-y-1">
            <Label>Project</Label>
            <Select value={String(project.id)} onValueChange={value => value && pickProject(Number(value))} items={projectItems}>
              <SelectTrigger aria-label="Project" className="min-w-52"><SelectValue /></SelectTrigger>
              <SelectContent>
                {projectItems.map(p => <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>)}
              </SelectContent>
            </Select>
            <FieldError name="Project" error={errors.project} />
          </div>
        </div>
        <div className="space-y-1">
          <Label>Kind</Label>
          <Segmented
            label="Kind"
            value={kind}
            options={[{ value: 'threshold', label: 'Threshold' }, { value: 'match', label: 'Match' }]}
            onChange={setKind}
          />
          <p className="text-xs text-muted-foreground">
            {isThreshold
              ? 'Runs its Query over a range and fires on a group\'s number: past a threshold, or changed by a percent from its Baseline. The Query ends in a summarize of one number.'
              : 'Reads the Logs ingested since its last run and sends one message when any match its Query.'}
          </p>
        </div>
      </div>

      <FormSection
        title="Query"
        actions={<Segmented label="Edit the Query in" size="sm" value={mode} options={[{ value: 'builder', label: 'Builder' }, { value: 'query', label: 'Query' }]} onChange={switchMode} />}
      >
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
            />
            {problems.map((p, i) => (
              <div key={i} className="mt-0.5 font-mono text-[11px] text-destructive">Line {p.line}, column {p.column}: {p.message}</div>
            ))}
          </div>
        )}
        <FieldError name="Query" error={errors.query} />
      </FormSection>

      <FormSection title={isThreshold ? 'When it fires' : 'When it runs'}>
        {isThreshold && comparesChange && (
          // The change rule reads as one sentence, beside the Compare that chose it.
          <div className="flex flex-wrap items-end gap-4">
            {compareField}
            <div className="min-w-0 space-y-1">
              <div className="flex min-h-8 flex-wrap items-center gap-x-2 gap-y-2 text-sm">
                <span>Fires when the number</span>
                <Select value={direction} onValueChange={value => value && setDirection(value as ChangeOperator)} items={CHANGE_OPERATORS.map(o => ({ value: o, label: CHANGE_WORDS[o] }))}>
                  <SelectTrigger aria-label="Direction" className="w-36"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {CHANGE_OPERATORS.map(o => <SelectItem key={o} value={o}>{CHANGE_WORDS[o]}</SelectItem>)}
                  </SelectContent>
                </Select>
                <span className="flex items-center gap-2">
                  <Input aria-label="Threshold percent" type="number" step="any" min={0} className="w-24" value={threshold} placeholder="50" onChange={e => setThreshold(e.target.value)} />
                  <span>% or more from</span>
                </span>
                <Select value={baseline} onValueChange={value => value && setBaseline(value as MonitorBaseline)} items={BASELINES}>
                  <SelectTrigger aria-label="Baseline" className="w-52"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {BASELINES.map(b => <SelectItem key={b.value} value={b.value}>{b.label}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              <FieldError name="Direction" error={errors.operator} />
              <FieldError name="Threshold" error={errors.threshold} />
              <FieldError name="Baseline" error={errors.baseline} />
            </div>
          </div>
        )}
        <div className="flex flex-wrap items-start gap-4">
          <div className="space-y-1">
            <Label htmlFor="monitor-interval">Run every (minutes)</Label>
            <Input id="monitor-interval" type="number" min={1} max={MAX_INTERVAL_MINUTES} className="w-32" value={intervalText} onChange={e => setIntervalText(e.target.value)} />
            <FieldError name="Interval" error={errors.interval_minutes} />
          </div>
          {isThreshold && (
            <>
              <div className="space-y-1">
                <Label htmlFor="monitor-range">Over the last (minutes)</Label>
                <Input id="monitor-range" type="number" min={1} max={MAX_RANGE_MINUTES} className="w-32" value={range} onChange={e => setRange(e.target.value)} />
                <FieldError name="Range" error={errors.range_minutes} />
              </div>
              {comparesChange ? (
                <div className="space-y-1">
                  <Label htmlFor="monitor-minimum">Minimum</Label>
                  <div className="flex items-center gap-2">
                    <Input id="monitor-minimum" type="number" step="any" min={0} className="w-28" value={minimum} placeholder="None" onChange={e => setMinimum(e.target.value)} />
                    {minimumValue !== null && <span className="text-xs text-muted-foreground" data-monitor-minimum-rule>{minimumText(minimumValue)}</span>}
                  </div>
                  <FieldError name="Minimum" error={errors.minimum} />
                </div>
              ) : (
                <>
                {compareField}
                <div className="space-y-1">
                  <Label>Fires when the number is</Label>
                  <div className="flex gap-2">
                    <Select value={operator} onValueChange={value => value && setOperator(value as ValueOperator)} items={OPERATORS.map(o => ({ value: o, label: o }))}>
                      <SelectTrigger aria-label="Operator" className="w-20 font-mono"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        {OPERATORS.map(o => <SelectItem key={o} value={o} className="font-mono">{o}</SelectItem>)}
                      </SelectContent>
                    </Select>
                    <Input aria-label="Threshold" type="number" step="any" className="w-32" value={threshold} placeholder="10" onChange={e => setThreshold(e.target.value)} />
                  </div>
                  <FieldError name="Operator" error={errors.operator} />
                  <FieldError name="Threshold" error={errors.threshold} />
                </div>
                </>
              )}
            </>
          )}
        </div>
        {isThreshold && comparesChange && (
          <p className="text-xs text-muted-foreground">Compares each group with {baselineRangeText(baseline, rangeMinutes)}.</p>
        )}
        {isThreshold && (
          <div className="space-y-2">
            <label className="flex items-center gap-2 text-sm">
              <Checkbox aria-label="Notify by group" checked={byGroup} onCheckedChange={checked => setByGroup(checked === true)} />
              Notify by group: each group of the summarize's by columns fires and resolves on its own
            </label>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox aria-label="Alert on no data" checked={noData} onCheckedChange={checked => setNoData(checked === true)} />
              Alert on no data: a run that finds no rows at all fires
            </label>
          </div>
        )}
      </FormSection>

      {isThreshold && (
        <FormSection
          title="Preview"
          description={previewCaption(rangeMinutes, comparesChange)}
          actions={<Button type="button" variant="outline" size="sm" onClick={() => setAgain(n => n + 1)}>Refresh preview</Button>}
        >
          <div className="min-h-48 border" data-monitor-preview>
            <RunView run={chart}>
              {({ table }) => (table.rows.length === 0
                ? <p className="p-3 text-xs text-muted-foreground">No rows over the last {CHART_HOURS} hours.</p>
                : <QueryChart table={table} form="line" threshold={comparesChange ? undefined : thresholdValue ?? undefined} className="pb-1" />)}
            </RunView>
          </div>
          <div className="space-y-1.5" data-monitor-groups>
            <h3 className="text-sm font-medium">If it ran now</h3>
            <RunView run={groupsRun}>
              {() => (!comparesChange && found?.error ? (
                <p role="alert" className="text-xs text-destructive">{found.error}</p>
              ) : listed.length === 0 ? (
                <p className="text-xs text-muted-foreground">Finds no rows, so nothing would fire{noData ? '' : ' (turn on Alert on no data to fire on that)'}.</p>
              ) : (
                <>
                  <p className="text-xs text-muted-foreground">
                    {firing.length === 0 ? 'Nothing would fire now.' : `${firing.length} ${firing.length === 1 ? 'group' : 'groups'} would fire now.`}
                    {thresholdValue === null && ' Set a threshold to compare with.'}
                  </p>
                  {comparesChange ? (
                    <div className="overflow-x-auto border">
                      <table className="w-full text-xs">
                        <thead className="text-muted-foreground">
                          <tr>
                            <th className="px-3 py-1.5 text-left font-medium">Group</th>
                            <th className="px-3 py-1.5 text-right font-medium">Value</th>
                            <th className="px-3 py-1.5 text-right font-medium">Baseline</th>
                            <th className="px-3 py-1.5 text-right font-medium">Change</th>
                            <th className="px-3 py-1.5"><span className="sr-only">Fires</span></th>
                          </tr>
                        </thead>
                        <tbody className="divide-y border-t">
                          {listed.slice(0, GROUPS_SHOWN).map(g => (
                            <tr key={JSON.stringify(g.group)} data-group-firing={g.firing}>
                              <td className="max-w-0 truncate px-3 py-1.5 font-mono w-full">{groupLabel(g.group)}</td>
                              <td className="whitespace-nowrap px-3 py-1.5 text-right font-mono tabular-nums" data-group-reading="value">
                                {g.value === null ? 'No data' : valueText(g.value)}
                              </td>
                              <td className="whitespace-nowrap px-3 py-1.5 text-right font-mono tabular-nums" data-group-reading="baseline">
                                {g.value === null || g.baseline === null ? '' : valueText(g.baseline)}
                              </td>
                              <td className={cn('whitespace-nowrap px-3 py-1.5 text-right font-mono tabular-nums', g.firing && 'text-destructive')} data-group-reading="change">
                                {g.value === null || g.baseline === null ? '' : changeText(g.value, g.baseline)}
                              </td>
                              <td className="whitespace-nowrap px-3 py-1.5 text-right font-medium text-destructive">{g.firing ? 'fires' : ''}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  ) : (
                    <ul className="divide-y border text-xs">
                      {listed.slice(0, GROUPS_SHOWN).map(g => (
                        <li key={JSON.stringify(g.group)} className="flex justify-between gap-3 px-3 py-1.5" data-group-firing={g.firing}>
                          <span className="min-w-0 truncate font-mono">{groupLabel(g.group)}</span>
                          <span className={cn('shrink-0 tabular-nums', g.firing && 'font-medium text-destructive')}>
                            {g.value === null ? 'No data' : <span className="font-mono">{valueText(g.value)}</span>}{g.firing ? ' · fires' : ''}
                          </span>
                        </li>
                      ))}
                    </ul>
                  )}
                  {listed.length > GROUPS_SHOWN && <p className="text-xs text-muted-foreground">and {listed.length - GROUPS_SHOWN} more</p>}
                </>
              ))}
            </RunView>
          </div>
        </FormSection>
      )}

      <FormSection
        title="Notify"
        description="The Integrations it tells when it fires and when it resolves."
        actions={
          <>
            <a href={`/${orgSlug}/settings/integrations/new`} target="_blank" rel="noreferrer" className="text-xs underline underline-offset-2">Add integration</a>
            <button type="button" className="text-xs underline underline-offset-2" onClick={() => router.reload({ only: ['integrations'] })}>Reload the list</button>
          </>
        }
      >
        {integrations.length === 0 ? (
          <p className="text-xs text-muted-foreground">The Organization has no Integrations yet. Without one, runs are recorded on the Monitor's page only.</p>
        ) : (
          <div className="space-y-2" role="group" aria-label="Integrations">
            {integrations.map(i => (
              <label key={i.id} className="flex items-center gap-2 text-sm">
                <Checkbox aria-label={i.name} checked={integrationIds.includes(i.id)} onCheckedChange={checked => toggleIntegration(i.id, checked === true)} />
                {i.name} <span className="text-xs text-muted-foreground">{providerName(i.provider)}{i.enabled ? '' : ', off'}</span>
              </label>
            ))}
          </div>
        )}
        <FieldError name="Integrations" error={errors.integrations} />
      </FormSection>

      <div className="flex justify-end gap-2 border-t pt-6">
        <Link href={monitorId === undefined ? `/${orgSlug}/monitors` : `/${orgSlug}/monitors/${monitorId}`} className={buttonVariants({ variant: 'outline' })}>Cancel</Link>
        <Button type="submit" disabled={saving}>{saving ? 'Saving…' : monitorId === undefined ? 'Create monitor' : 'Save monitor'}</Button>
      </div>
    </form>
  )
}

/** A field's errors from the server, each after the field's name, wrapping within the field's width rather than widening it. */
function FieldError({ name, error }: { name: string; error: unknown }) {
  const messages = (Array.isArray(error) ? error : error ? [error] : []).map(String)
  return (
    <>
      {messages.map((m, i) => <p key={i} role="alert" className="w-0 min-w-full text-xs text-destructive">{name} {m}</p>)}
    </>
  )
}

function RunView<R extends Run | GroupsRun>({ run, children }: { run: R; children: (done: Extract<R, { status: 'done' }>) => React.ReactNode }) {
  if (run.status === 'running') {
    return <ChartSkeleton label="Running the preview" />
  }
  if (run.status === 'error') return <p role="alert" className="p-3 text-xs text-destructive">{run.message}</p>
  return <>{children(run as Extract<R, { status: 'done' }>)}</>
}

/** A whole number typed in a field, within its bounds, or the low bound when it isn't one. */
function wholeIn(text: string, low: number, high: number): number {
  const n = Math.round(Number(text))
  return Number.isFinite(n) ? Math.min(high, Math.max(low, n)) : low
}
