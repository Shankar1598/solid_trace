import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { router, usePage } from '@inertiajs/react'
import { Play, Square } from 'lucide-react'
import { toast } from 'sonner'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { TimeRangePicker } from '@/components/TimeRangePicker'
import { copyText } from '@/components/copyText'
import { useShortcut } from '@/components/useShortcut'
import { Button } from '@/components/ui/button'
import { vizOfForm } from '@/lib/dashboards/dashboard'
import { RunError, fetchHistogram, fetchLogPage, fetchReadableSql, fetchRun, runSql, validateQuery, type QueryProblem } from '@/lib/logs/api'
import {
  EMPTY_BUILDER,
  addFilter,
  builderOf,
  chartField,
  groupBy,
  queryOf,
  type Builder,
  type Chip,
} from '@/lib/logs/builder'
import {
  chooseViz,
  fillEmptyQuery,
  linkSearch,
  modesFor,
  openExplorer,
  pickRange,
  resolveRange,
  runExplorer,
  runsOnOpen,
  searchOf,
  switchMode,
  type Explorer,
  type ExplorerState,
  type LinkKind,
  type Mode,
  type TimeRange,
  type Viz,
  typeQuery,
  typeSql,
} from '@/lib/logs/explorerState'
import { binsTime, chartOf } from '@/lib/charts/series'
import { columnsFromSearch, commonValues, fieldInfos, rememberLogs, toggleColumn, withColumns } from '@/lib/logs/columns'
import { errorParts } from '@/lib/logs/errorParts'
import { bucketSeconds, filterByLevel, type HistogramLevel } from '@/lib/logs/histogram'
import { editAsBuilder, pivotQuery, toBuilder } from '@/lib/logs/queryMode'
import { discardSql, enterSql, leaveSql } from '@/lib/logs/sqlMode'
import { fillStream, keepsFetching, startStream, streamEnd, wantOlder, type Log, type Stream } from '@/lib/logs/stream'
import type { LogTable } from '@/lib/logs/table'
import { registerPaletteSource } from '@/lib/palette'
import { browserStorage, recordQuery } from '@/lib/recentQueries'
import { SharedProps } from '@/types'
import { BuilderBar, type BuilderView } from './components/BuilderBar'
import { BuilderPanel, type ProjectOption } from './components/BuilderPanel'
import { AddToDashboard } from './components/AddToDashboard'
import { CreateMonitor } from './components/CreateMonitor'
import { CopyLink } from './components/CopyLink'
import { HistogramStrip, type HistogramView } from './components/HistogramStrip'
import { LogInspector } from './components/LogInspector'
import { LogStream } from './components/LogStream'
import { LogTableView, type TableChart } from './components/LogTableView'
import { ModeSwitch } from './components/ModeSwitch'
import { QueryBox, type LeavingQuery } from './components/QueryBox'
import { SqlBox } from './components/SqlBox'
import { TagMenuProvider, type OnFilter, type OnSummarize } from './components/TagMenu'

interface LogsIndexProps {
  project: ProjectOption
  projects: ProjectOption[]
  sql_queries_enabled: boolean
}

type Result =
  | { status: 'waiting' }
  | { status: 'running' }
  | { status: 'cancelled' }
  | { status: 'error'; message: string }
  | { status: 'done'; stream: Stream; loadingOlder: boolean; olderError: string | null }
  | { status: 'table'; table: LogTable; elapsedMs: number | null }

/** One run: its range resolved once, so older pages continue the same window. */
interface Run {
  controller: AbortController
  query: string
  from: Date
  to: Date
}

export default function LogsIndex({ project, projects, sql_queries_enabled }: LogsIndexProps) {
  const { current_org: currentOrg, current_user: currentUser } = usePage<SharedProps>().props
  const orgSlug = currentOrg?.slug ?? ''
  const viewer = currentUser?.id ?? null
  const basePath = `/${orgSlug}/projects/${project.slug}/logs`
  const modes = modesFor(sql_queries_enabled)
  // The Query of the Project's Logs, unfiltered: what an empty Query shows as in Query mode.
  const projectQuery = queryOf(EMPTY_BUILDER, project.slug)

  // Back remounts the page (Inertia restores it without preserving state), so
  // reading the URL once here is enough to undo the last run.
  const [explorer, setExplorer] = useState<Explorer>(() => fillEmptyQuery(openExplorer(window.location.search, modes), projectQuery))
  // For answers that arrive after a render: what the explorer is by then.
  const latest = useRef(explorer)
  latest.current = explorer
  // SQL from a URL waits for Run.
  const [result, setResult] = useState<Result>(() => runsOnOpen(explorer) ? { status: 'running' } : { status: 'waiting' })
  const [histogram, setHistogram] = useState<HistogramView>({ status: 'running' })
  const [builderView, setBuilderView] = useState<BuilderView>(() =>
    explorer.current.query === '' || explorer.current.query === projectQuery ? { status: 'ready', builder: EMPTY_BUILDER } : { status: 'reading' })
  const [inspected, setInspected] = useState<Log | null>(null)
  // Query mode: the mistakes of the Query last run, and the prompt to leave.
  const [problems, setProblems] = useState<QueryProblem[]>([])
  const [leaving, setLeaving] = useState<LeavingQuery | null>(null)
  // SQL mode: the readable SQL it opened with (null when it opened from a
  // URL), and the mode a member asked to leave for with edited SQL.
  const [sqlSeed, setSqlSeed] = useState<string | null>(null)
  const [leavingSql, setLeavingSql] = useState<Mode | null>(null)
  const currentRun = useRef<Run | null>(null)
  // The stream's columns: in the URL beside the explorer's state, but not part
  // of a run, so changing them replaces the history entry.
  const [columns, setColumns] = useState<string[]>(() => columnsFromSearch(window.location.search))
  const shownColumns = useRef(columns)
  shownColumns.current = columns

  // Shows a run's stream, and fetches older pages into it while EventStore
  // answers short of a page: a search for rare Logs answers after a few
  // seconds with what it has found so far.
  const fill = useCallback((run: Run, stream: Stream) => {
    const show = (s: Stream) => {
      if (currentRun.current === run) setResult({ status: 'done', stream: s, loadingOlder: keepsFetching(s), olderError: null })
    }
    show(stream)
    fillStream(stream, cursor => fetchLogPage({
      orgSlug, projectSlug: project.slug, query: run.query, from: run.from, to: run.to, cursor, signal: run.controller.signal,
    }), show)
      .catch((error: Error) => {
        if (currentRun.current !== run || run.controller.signal.aborted) return
        setResult(r => r.status === 'done' ? { ...r, loadingOlder: false, olderError: error.message } : r)
      })
  }, [orgSlug, project.slug])

  const start = useCallback((state: ExplorerState) => {
    currentRun.current?.controller.abort()
    const run: Run = { controller: new AbortController(), query: state.query, ...resolveRange(state.range, new Date()) }
    currentRun.current = run
    setResult({ status: 'running' })
    // Each run of a Query is a recent one; the Project's Logs unfiltered are not.
    if (viewer !== null && state.mode !== 'sql' && state.query !== '' && state.query !== projectQuery) {
      recordQuery(browserStorage(), viewer, orgSlug, project.slug, { query: state.query, search: searchOf(state), at: Date.now() })
    }

    // SQL answers a table, and has no histogram.
    if (state.mode === 'sql') {
      runSql({ orgSlug, projectSlug: project.slug, sql: state.sql, from: run.from, to: run.to, signal: run.controller.signal })
        .then(({ table, elapsedMs }) => {
          if (currentRun.current === run) setResult({ status: 'table', table, elapsedMs })
        })
        .catch((error: Error) => {
          if (currentRun.current === run && !run.controller.signal.aborted) setResult({ status: 'error', message: error.message })
        })
      return
    }

    setHistogram({ status: 'running' })

    // The histogram and summary count the same filters over the same range.
    fetchHistogram({
      orgSlug, projectSlug: project.slug, query: run.query, from: run.from, to: run.to,
      bucketSeconds: bucketSeconds(run.from, run.to), signal: run.controller.signal,
    })
      .then(histogram => {
        if (currentRun.current === run) setHistogram({ status: 'done', histogram, from: run.from, to: run.to })
      })
      .catch((error: Error) => {
        if (currentRun.current === run && !run.controller.signal.aborted) setHistogram({ status: 'error', message: error.message })
      })

    fetchRun({ orgSlug, projectSlug: project.slug, query: run.query, from: run.from, to: run.to, signal: run.controller.signal })
      .then(answer => {
        if (currentRun.current !== run) return
        if (answer.kind === 'table') setResult({ status: 'table', table: answer.table, elapsedMs: answer.elapsedMs })
        else fill(run, startStream(answer.page))
      })
      .catch((error: Error) => {
        if (currentRun.current !== run || run.controller.signal.aborted) return
        setResult({ status: 'error', message: error.message })
        if (error instanceof RunError && error.problems.length > 0) setProblems(error.problems)
      })
  }, [viewer, orgSlug, project.slug, projectQuery, fill])

  useEffect(() => {
    if (runsOnOpen(explorer)) start(explorer.current)
    return () => currentRun.current?.controller.abort()
    // Runs once per mount: later runs start from the bar.
  }, [])

  // The Builder reads chips only from validate: the browser never parses a
  // Query, even one it wrote. A Query it can't show opens in Query mode, and
  // the Builder starts empty behind it.
  useEffect(() => {
    if (builderView.status !== 'reading') return
    const query = explorer.current.query
    const controller = new AbortController()
    const unshowable = () => {
      setBuilderView(v => v.status === 'reading' ? { status: 'ready', builder: EMPTY_BUILDER } : v)
      if (latest.current.current.mode !== 'builder') return
      const { explorer: next, replace } = switchMode(latest.current, 'query')
      setExplorer(next)
      replaceUrl(replace)
    }
    validateQuery({ orgSlug, projectSlug: project.slug, query, signal: controller.signal })
      .then(({ chips }) => {
        if (chips) setBuilderView(v => v.status === 'reading' ? { status: 'ready', builder: builderOf(chips) } : v)
        else unshowable()
      })
      .catch(() => {
        if (!controller.signal.aborted) unshowable()
      })
    return () => controller.abort()
    // Once per mount, like the first run.
  }, [])

  const urlOf = (search: string) => basePath + withColumns(search, shownColumns.current)
  const replaceUrl = (search: string) => router.replace({ url: urlOf(search), preserveState: true, preserveScroll: true })

  const changeColumns = useCallback((next: string[]) => {
    shownColumns.current = next
    setColumns(next)
    router.replace({ url: basePath + withColumns(searchOf(latest.current.current), next), preserveState: true, preserveScroll: true })
  }, [basePath])

  const run = (next: Explorer) => {
    const { explorer: ran, push } = runExplorer(next)
    setExplorer(ran)
    if (push) router.push({ url: urlOf(push), preserveState: true, preserveScroll: true })
    start(ran.current)
  }

  /**
   * Runs the draft. In Query mode a Query with mistakes doesn't run: they
   * show under the Query box, and the last good result stays.
   */
  const commit = (next: Explorer) => {
    setLeaving(null)
    setLeavingSql(null)
    if (next.draft.mode !== 'query') return run(next)
    validateQuery({ orgSlug, projectSlug: project.slug, query: next.draft.query })
      .then(({ errors, source }) => {
        // A Query naming another of the Organization's Projects runs there.
        const named = source !== project.slug ? projects.find(p => p.slug === source) : undefined
        if (named) return router.get(`/${orgSlug}/projects/${named.slug}/logs${withColumns(searchOf(next.draft), columns)}`)
        setProblems(errors)
        if (errors.length === 0) run(next)
      })
      .catch(() => {
        // Validate is out of reach: the run will say what's wrong.
        setProblems([])
        run(next)
      })
  }

  /** Into the Builder, showing builder; its Query runs when it isn't what ran. */
  const enterBuilder = (builder: Builder, runIt: boolean) => {
    setLeaving(null)
    setProblems([])
    setBuilderView({ status: 'ready', builder })
    const { explorer: next, replace } = switchMode(latest.current, 'builder')
    if (runIt) {
      commit(typeQuery(next, queryOf(builder, project.slug)))
    } else {
      setExplorer(next)
      replaceUrl(replace)
    }
  }

  /**
   * Builder → Query keeps the Builder's Query as text. Query → Builder is
   * silent when the Builder can show the text; otherwise it asks first. Into
   * SQL opens the readable SQL of the Query; out of SQL asks first when the
   * SQL was edited.
   */
  const switchTo = (mode: Mode) => {
    if (explorer.current.mode === 'sql') {
      const answer = leaveSql(explorer, sqlSeed, mode)
      if (answer.kind === 'ask') setLeavingSql(answer.mode)
      else exitSql(mode)
      return
    }
    if (mode === 'sql') return openSql()
    if (mode === 'query') {
      const next = fillEmptyQuery(switchMode(explorer, 'query').explorer, projectQuery)
      setExplorer(next)
      replaceUrl(searchOf(next.current))
      setProblems([])
      return
    }
    const query = explorer.draft.query
    validateQuery({ orgSlug, projectSlug: project.slug, query })
      .then(validation => {
        const answer = toBuilder(query, validation)
        if (answer.kind === 'switch') enterBuilder(answer.builder, query !== latest.current.current.query)
        else setLeaving({ stages: answer.stages, problems: answer.problems })
      })
      .catch((error: Error) => toast.error(error.message))
  }

  /** Into SQL mode with the readable SQL of the Query being edited; nothing runs until Run. */
  const openSql = () => {
    const { query, range } = explorer.draft
    fetchReadableSql({ orgSlug, projectSlug: project.slug, query, ...resolveRange(range, new Date()) })
      .then(sql => {
        const { explorer: next, replace } = enterSql(latest.current, sql)
        setExplorer(next)
        replaceUrl(replace)
        setSqlSeed(sql)
        setLeaving(null)
        setProblems([])
        currentRun.current?.controller.abort()
        currentRun.current = null
        setResult({ status: 'waiting' })
      })
      .catch((error: Error) => {
        if (error instanceof RunError && error.problems.length > 0 && explorer.current.mode === 'query') setProblems(error.problems)
        toast.error(`SQL mode needs a Query without mistakes. ${error.message}`)
      })
  }

  /**
   * Out of SQL mode, dropping the SQL, back to the Query it came from, which
   * runs again in place of the SQL's result. A Query the Builder can't show
   * lands in Query mode, asking as Query → Builder does.
   */
  const exitSql = (mode: Mode) => {
    setLeavingSql(null)
    setSqlSeed(null)
    const leave = (to: Mode) => {
      const next = fillEmptyQuery(discardSql(latest.current, to).explorer, projectQuery)
      latest.current = next
      setExplorer(next)
      replaceUrl(searchOf(next.current))
      start(next.current)
    }
    if (mode !== 'builder') return leave(mode)

    const query = explorer.current.query
    validateQuery({ orgSlug, projectSlug: project.slug, query })
      .then(validation => {
        const answer = toBuilder(query, validation)
        if (answer.kind === 'switch') {
          setBuilderView({ status: 'ready', builder: answer.builder })
          leave('builder')
        } else {
          leave('query')
          setLeaving({ stages: answer.stages, problems: answer.problems })
        }
      })
      .catch(() => leave('query'))
  }

  /** Discard and switch: back to the Builder's last filters. */
  const discardQuery = () => {
    const builder = builderView.status === 'ready' ? builderView.builder : EMPTY_BUILDER
    enterBuilder(builder, queryOf(builder, project.slug) !== explorer.current.query)
  }

  /**
   * Group by or Chart this in Query mode: the Builder's edit of the Query
   * that ran, so its Summarize changes rather than a second one following it.
   */
  const summarizeQuery = (edit: (builder: Builder) => Builder) => {
    const query = explorer.current.query || projectQuery
    validateQuery({ orgSlug, projectSlug: project.slug, query })
      .then(validation => {
        const next = editAsBuilder(validation, project.slug, edit)
        if (next === null) return toast.error('Only a Query the Builder can show can be grouped or charted from here. Add a summarize to the Query instead.')
        setInspected(null)
        commit(typeQuery(latest.current, next))
      })
      .catch((error: Error) => toast.error(error.message))
  }

  /** A pivot in Query mode: a where after the leading filters of the Query that ran. */
  const pivot = (chip: Chip) => {
    // An empty Query reads the Project's Logs, as its slug alone does: the where goes after the slug.
    const query = explorer.current.query || projectQuery
    validateQuery({ orgSlug, projectSlug: project.slug, query })
      .then(({ errors, stages }) => {
        if (errors.length > 0) toast.error('Fix the Query before filtering from its results.')
        else commit(typeQuery(latest.current, pivotQuery(query, stages, chip)))
      })
      .catch((error: Error) => toast.error(error.message))
  }

  /** Edits the Builder; its Query text becomes the draft, run at once unless only typed. */
  const changeBuilder = (builder: Builder, run: boolean) => {
    setBuilderView({ status: 'ready', builder })
    const next = typeQuery(explorer, queryOf(builder, project.slug))
    if (run) commit(next)
    else setExplorer(next)
  }

  const mode = explorer.current.mode
  const sqlMode = mode === 'sql'
  const builder = mode === 'builder' && builderView.status === 'ready' ? builderView.builder : null
  let addToQuery: ((chip: Chip) => void) | null = null
  let editSummarize: ((edit: (builder: Builder) => Builder) => void) | null = null
  let onLevel: ((level: HistogramLevel) => void) | null = null
  if (builder) {
    addToQuery = chip => changeBuilder(addFilter(builder, chip), true)
    // A Summarize shows a table, which has no Logs to inspect.
    editSummarize = edit => {
      setInspected(null)
      changeBuilder(edit(builder), true)
    }
    onLevel = level => {
      const filtered = filterByLevel(builder, level)
      if (filtered) changeBuilder(filtered, true)
    }
  } else if (mode === 'query') {
    addToQuery = pivot
    editSummarize = summarizeQuery
    onLevel = level => {
      if (level !== 'none') pivot({ field: 'level', op: '==', value: level })
    }
  }
  const filterWith = addToQuery
  const onFilter: OnFilter = filterWith && ((kind, field, value) => filterWith({ field, op: kind === 'show' ? '==' : '!=', value }))
  const summarizeWith = editSummarize
  const groupByField = summarizeWith && ((field: string) => summarizeWith(b => groupBy(b, field)))
  const onSummarize: OnSummarize = summarizeWith && ((kind, field, numeric) =>
    summarizeWith(b => (kind === 'group' ? groupBy(b, field) : chartField(b, field, numeric))))
  const columnControl = { shown: columns, toggle: (field: string) => changeColumns(toggleColumn(columns, field)) }

  // The inspector steps through the stream's Logs, newest first.
  const streamLogs = result.status === 'done' ? result.stream.logs : []
  const inspectedAt = inspected ? streamLogs.findIndex(l => l.id === inspected.id) : -1
  const onNewer = inspectedAt > 0 ? () => setInspected(streamLogs[inspectedAt - 1]) : null
  const onOlder = inspectedAt >= 0 && inspectedAt < streamLogs.length - 1 ? () => setInspected(streamLogs[inspectedAt + 1]) : null

  // SQL runs only on Run, so in SQL mode a picked range waits for it.
  const pickBarRange = (range: TimeRange) => sqlMode ? setExplorer(pickRange(explorer, range)) : commit(pickRange(explorer, range))

  const closeInspector = useCallback(() => setInspected(null), [])

  /** Copies a link to what ran: as shown, over the range the run used, or live. */
  const copyLink = (kind: LinkKind) => {
    const run = currentRun.current
    const ran = run ?? resolveRange(explorer.current.range, new Date())
    copyText(window.location.origin + urlOf(linkSearch(explorer.current, kind, ran)), 'Link')
  }
  const copyLinkNow = useRef(copyLink)
  copyLinkNow.current = copyLink
  // The palette's Copy link copies the URL, which is the live link here; this adds as shown.
  useEffect(() => registerPaletteSource('explorer', () => [
    { id: 'explorer/link-shown', group: 'Actions', label: 'Copy link as shown', detail: "with the run's times", run: () => copyLinkNow.current('shown') },
  ]), [])

  // Cmd/Ctrl+Enter runs from anywhere on the page; the editors run on their own.
  useShortcut('run', () => commit(explorer))

  /** Draws tables another way: the URL keeps it, and nothing runs. */
  const changeViz = (viz: Viz) => {
    const { explorer: next, replace } = chooseViz(latest.current, viz)
    latest.current = next
    setExplorer(next)
    replaceUrl(replace)
  }
  // Series keep their colours across runs, so a filter or a zoom repaints none of those left,
  // and a series hidden from the legend stays hidden.
  const colorSlots = useRef<ReadonlyMap<string, number>>(new Map())
  const hiddenSeries = useRef<ReadonlySet<string>>(new Set())
  // Add to dashboard saves the Query that ran, drawn as shown, and Create monitor starts from it and
  // its range; SQL isn't a Query, so it has neither.
  const ranQuery = explorer.current.query
  const tableChart: TableChart = {
    viz: explorer.current.viz, onViz: changeViz, onZoom: pickBarRange, colors: colorSlots, hidden: hiddenSeries,
    beside: sqlMode ? undefined : shown => (
      <div className="flex items-center gap-2">
        <AddToDashboard projectId={project.id} query={ranQuery} viz={vizOfForm(shown)} />
        <CreateMonitor projectId={project.id} query={ranQuery} range={explorer.current.range} />
      </div>
    ),
  }

  // A table over time stands in for the histogram strip, as in Axiom, shown
  // as a chart or as Table alike, so the switch never moves under the
  // pointer. The last answer decides, through a re-run too, so the strip
  // doesn't flash back while a chart's Query runs again; before a first
  // answer, the Query's text guesses.
  const answered = useRef<LogTable | null>(null)
  if (result.status === 'table') answered.current = result.table
  else if (result.status !== 'running' && result.status !== 'cancelled') answered.current = null
  const answeredChart = useMemo(() => (answered.current ? chartOf(answered.current) : null), [answered.current])
  const timeChart = answeredChart
    ? answeredChart.kind === 'time'
    : result.status === 'running' && binsTime(explorer.current.query)

  const cancel = () => {
    currentRun.current?.controller.abort()
    // A search keeps the Logs it found.
    setResult(r => r.status === 'done' && r.stream.logs.length > 0
      ? { ...r, loadingOlder: false, olderError: 'the search was cancelled.' }
      : { status: 'cancelled' })
    setHistogram(h => h.status === 'running' ? { status: 'cancelled' } : h)
  }

  const loadOlder = useCallback(() => {
    const run = currentRun.current
    if (!run || result.status !== 'done' || result.loadingOlder) return
    // After Cancel, Try again goes on with the same run.
    if (run.controller.signal.aborted) run.controller = new AbortController()
    fill(run, wantOlder(result.stream))
  }, [fill, result])

  /** The Builder's Project: its clauses, on the Logs of the Project picked. */
  const openProject = (slug: string) => {
    if (slug === project.slug) return
    const query = builder ? queryOf(builder, slug) : explorer.current.query
    router.get(`/${orgSlug}/projects/${slug}/logs${withColumns(searchOf({ ...explorer.current, query }), columns)}`)
  }

  // The Fields and values the Builder suggests: those of recent runs' Logs, kept while a table shows.
  const seen = useRef<{ from: Log[] | null; logs: Log[] }>({ from: null, logs: [] })
  if (result.status === 'done' && result.stream.logs.length > 0 && result.stream.logs !== seen.current.from) {
    seen.current = { from: result.stream.logs, logs: rememberLogs(seen.current.logs, result.stream.logs) }
  }
  const fields = useMemo(() => fieldInfos(seen.current.logs), [seen.current.logs])
  const valuesOf = useCallback((field: string) => commonValues(seen.current.logs, field), [seen.current.logs])

  const running = result.status === 'running' || (result.status === 'done' && result.loadingOlder)

  return (
    <DashboardLayout>
      <div className="h-[calc(100vh-2rem)] flex flex-col rounded-md border overflow-hidden bg-card">
        <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
          <TimeRangePicker range={explorer.draft.range} onChange={pickBarRange} retentionDays={project.log_retention_days} />
          <ModeSwitch modes={modes} mode={mode} onSwitch={switchTo} />
          <div className="h-5 w-px bg-border" />
          {mode === 'builder' ? (
            <BuilderBar view={builderView} />
          ) : (
            <span className="flex-1 text-[11px] text-muted-foreground">Ctrl+Enter runs the {sqlMode ? 'SQL' : 'Query'}.</span>
          )}
          <CopyLink onCopy={copyLink} />
          {running ? (
            <Button variant="outline" size="sm" onClick={cancel}><Square className="h-3.5 w-3.5" /> Cancel</Button>
          ) : (
            <Button variant="outline" size="sm" onClick={() => commit(explorer)}><Play className="h-3.5 w-3.5" /> Run</Button>
          )}
        </div>

        {builder && (
          <BuilderPanel
            builder={builder}
            project={project}
            projects={projects}
            onProject={openProject}
            fields={fields}
            valuesOf={valuesOf}
            onChange={b => changeBuilder(b, true)}
          />
        )}

        {mode === 'query' && (
          <QueryBox
            projectSlug={project.slug}
            text={explorer.draft.query}
            onChange={text => {
              setExplorer(typeQuery(explorer, text))
              setLeaving(null)
            }}
            onRun={() => commit(explorer)}
            problems={problems}
            completions={{ fields, valuesOf, source: projectQuery }}
            leaving={leaving}
            onDiscard={discardQuery}
            onStay={() => setLeaving(null)}
          />
        )}

        {sqlMode && (
          <SqlBox
            text={explorer.draft.sql}
            onChange={text => {
              setExplorer(typeSql(explorer, text))
              setLeavingSql(null)
            }}
            onRun={() => commit(explorer)}
            leaving={leavingSql !== null}
            onDiscard={() => leavingSql && exitSql(leavingSql)}
            onStay={() => setLeavingSql(null)}
          />
        )}

        {!sqlMode && !timeChart && (
          <HistogramStrip view={histogram} onZoom={range => commit(pickRange(explorer, range))} onLevel={onLevel} />
        )}

        <div className="relative flex-1 min-h-0">
          <TagMenuProvider onFilter={onFilter} onSummarize={onSummarize} columns={columnControl}>
            <ResultArea
              result={result}
              sql={sqlMode}
              chart={tableChart}
              onLoadOlder={loadOlder}
              selectedId={inspected?.id ?? null}
              onSelect={setInspected}
              columns={columns}
              onColumns={changeColumns}
            />
            {inspected && (
              <LogInspector
                key={inspected.id}
                log={inspected}
                orgSlug={orgSlug}
                projectSlug={project.slug}
                actions={{ filter: addToQuery, groupBy: groupByField, columns: columnControl }}
                onNewer={onNewer}
                onOlder={onOlder}
                onClose={closeInspector}
              />
            )}
          </TagMenuProvider>
        </div>
      </div>
    </DashboardLayout>
  )
}

/** The run's result; `sql` while SQL mode's SQL ran, which answers a table rather than Logs. */
function ResultArea({ result, sql, chart, onLoadOlder, selectedId, onSelect, columns, onColumns }: {
  result: Result; sql: boolean; chart: TableChart; onLoadOlder: () => void; selectedId: string | null; onSelect: (log: Log) => void
  columns: string[]; onColumns: (columns: string[]) => void
}) {
  switch (result.status) {
    case 'waiting':
      return <Notice>Press Run to run the SQL.</Notice>
    case 'running':
      return <Notice>{sql ? 'Running the SQL…' : 'Running the Query…'}</Notice>
    case 'cancelled':
      return <Notice>{sql ? 'SQL cancelled. Run it again.' : 'Query cancelled. Run it again to see Logs.'}</Notice>
    case 'error':
      return <ErrorNotice message={result.message} />
    case 'table':
      return <LogTableView table={result.table} elapsedMs={result.elapsedMs} chart={chart} />
    case 'done':
      if (result.stream.logs.length === 0) {
        return result.loadingOlder
          ? <Notice>Searching… no matching Logs yet.</Notice>
          : <Notice>No Logs match. Widen the time range or change the Query.</Notice>
      }
      return (
        <LogStream
          logs={result.stream.logs}
          end={streamEnd(result.stream)}
          loadingOlder={result.loadingOlder}
          olderError={result.olderError}
          onLoadOlder={onLoadOlder}
          selectedId={selectedId}
          onSelect={onSelect}
          columns={columns}
          onColumns={onColumns}
        />
      )
  }
}

function Notice({ children }: { children: React.ReactNode }) {
  return <div className="p-10 text-center text-sm text-muted-foreground">{children}</div>
}

/**
 * A run's error: its lines left-aligned as one block. DuckDB's LINE and caret
 * keep their columns, scrolling sideways when they don't fit, so the ^ stays
 * under the mistake it points at; the prose around them wraps.
 */
function ErrorNotice({ message }: { message: string }) {
  return (
    <div role="alert" className="p-10 text-sm text-destructive">
      <div className="mx-auto w-fit max-w-full space-y-5">
        {errorParts(message).map((part, i) => part.kind === 'pointer'
          ? <PointerLines key={i} text={part.text} />
          : <p key={i} className="whitespace-pre-wrap break-words">{part.text}</p>)}
      </div>
    </div>
  )
}

/** DuckDB's LINE and caret, scrolled to the caret when a phone draws it past the right edge. */
function PointerLines({ text }: { text: string }) {
  const scrollToCaret = useCallback((pre: HTMLPreElement | null) => {
    const node = pre?.firstChild
    if (!pre || !node || pre.scrollWidth <= pre.clientWidth) return
    const caret = document.createRange()
    caret.setStart(node, text.length - 1)
    caret.setEnd(node, text.length)
    const left = caret.getBoundingClientRect().left - pre.getBoundingClientRect().left + pre.scrollLeft
    pre.scrollLeft = left - pre.clientWidth / 2
  }, [text])
  return <pre ref={scrollToCaret} className="overflow-x-auto font-sans">{text}</pre>
}
