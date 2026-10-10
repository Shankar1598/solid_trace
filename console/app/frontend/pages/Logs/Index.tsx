import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { router, usePage } from '@inertiajs/react'
import { format } from 'date-fns'
import { Play, Square } from 'lucide-react'
import { toast } from 'sonner'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { RunError, fetchHistogram, fetchLogPage, fetchReadableSql, fetchRun, runSql, validateQuery, type QueryProblem } from '@/lib/logs/api'
import {
  EMPTY_BUILDER,
  addFilter,
  builderOf,
  groupBy,
  queryOf,
  type Builder,
  type Chip,
} from '@/lib/logs/builder'
import {
  RANGE_PRESETS,
  modesFor,
  openExplorer,
  pickRange,
  resolveRange,
  runExplorer,
  runsOnOpen,
  searchOf,
  spanLabel,
  switchMode,
  type Explorer,
  type ExplorerState,
  type Mode,
  type TimeRange,
  typeQuery,
  typeSql,
} from '@/lib/logs/explorerState'
import { columnsFromSearch, commonValues, fieldInfos, rememberLogs, toggleColumn, withColumns } from '@/lib/logs/columns'
import { bucketSeconds, filterByLevel, type HistogramLevel } from '@/lib/logs/histogram'
import { pivotQuery, toBuilder } from '@/lib/logs/queryMode'
import { discardSql, enterSql, leaveSql } from '@/lib/logs/sqlMode'
import { fillStream, keepsFetching, startStream, streamEnd, wantOlder, type Log, type Stream } from '@/lib/logs/stream'
import type { LogTable } from '@/lib/logs/table'
import { SharedProps } from '@/types'
import { BuilderBar, type BuilderView } from './components/BuilderBar'
import { BuilderPanel, type ProjectOption } from './components/BuilderPanel'
import { HistogramStrip, type HistogramView } from './components/HistogramStrip'
import { LogInspector } from './components/LogInspector'
import { LogStream } from './components/LogStream'
import { LogTableView } from './components/LogTableView'
import { ModeSwitch } from './components/ModeSwitch'
import { QueryBox, type LeavingQuery } from './components/QueryBox'
import { SqlBox } from './components/SqlBox'
import { TagMenuProvider, type OnFilter } from './components/TagMenu'

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
  | { status: 'table'; table: LogTable }

/** One run: its range resolved once, so older pages continue the same window. */
interface Run {
  controller: AbortController
  query: string
  from: Date
  to: Date
}

export default function LogsIndex({ project, projects, sql_queries_enabled }: LogsIndexProps) {
  const orgSlug = usePage<SharedProps>().props.current_org?.slug ?? ''
  const basePath = `/${orgSlug}/projects/${project.slug}/logs`
  const modes = modesFor(sql_queries_enabled)

  // Back remounts the page (Inertia restores it without preserving state), so
  // reading the URL once here is enough to undo the last run.
  const [explorer, setExplorer] = useState<Explorer>(() => {
    const opened = openExplorer(window.location.search, modes)
    // An empty Query opens as the Project's slug, as switching to Query mode writes it.
    return opened.current.mode === 'query' && opened.draft.query === ''
      ? typeQuery(opened, queryOf(EMPTY_BUILDER, project.slug))
      : opened
  })
  // For answers that arrive after a render: what the explorer is by then.
  const latest = useRef(explorer)
  latest.current = explorer
  // SQL from a URL waits for Run.
  const [result, setResult] = useState<Result>(() => runsOnOpen(explorer) ? { status: 'running' } : { status: 'waiting' })
  const [histogram, setHistogram] = useState<HistogramView>({ status: 'running' })
  const [builderView, setBuilderView] = useState<BuilderView>(() =>
    explorer.current.query === '' ? { status: 'ready', builder: EMPTY_BUILDER } : { status: 'reading' })
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

    // SQL answers a table, and has no histogram.
    if (state.mode === 'sql') {
      runSql({ orgSlug, projectSlug: project.slug, sql: state.sql, from: run.from, to: run.to, signal: run.controller.signal })
        .then(table => {
          if (currentRun.current === run) setResult({ status: 'table', table })
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
        if (answer.kind === 'table') setResult({ status: 'table', table: answer.table })
        else fill(run, startStream(answer.page))
      })
      .catch((error: Error) => {
        if (currentRun.current !== run || run.controller.signal.aborted) return
        setResult({ status: 'error', message: error.message })
        if (error instanceof RunError && error.problems.length > 0) setProblems(error.problems)
      })
  }, [orgSlug, project.slug, fill])

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
      const { explorer: next, replace } = switchMode(explorer, 'query')
      // An empty Query opens as the Project's slug, to write stages after.
      setExplorer(next.draft.query === '' ? typeQuery(next, queryOf(EMPTY_BUILDER, project.slug)) : next)
      replaceUrl(replace)
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
      const { explorer: next, replace } = discardSql(latest.current, to)
      latest.current = next
      setExplorer(next)
      replaceUrl(replace)
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

  /** A pivot in Query mode: a where after the leading filters of the Query that ran. */
  const pivot = (chip: Chip) => {
    // An empty Query reads the Project's Logs, as its slug alone does: the where goes after the slug.
    const query = explorer.current.query || queryOf(EMPTY_BUILDER, project.slug)
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
  let groupByField: ((field: string) => void) | null = null
  let onLevel: ((level: HistogramLevel) => void) | null = null
  if (builder) {
    addToQuery = chip => changeBuilder(addFilter(builder, chip), true)
    // A Summarize shows a table, which has no Logs to inspect.
    groupByField = field => {
      setInspected(null)
      changeBuilder(groupBy(builder, field), true)
    }
    onLevel = level => {
      const filtered = filterByLevel(builder, level)
      if (filtered) changeBuilder(filtered, true)
    }
  } else if (mode === 'query') {
    addToQuery = pivot
    onLevel = level => {
      if (level !== 'none') pivot({ field: 'level', op: '==', value: level })
    }
  }
  const filterWith = addToQuery
  const onFilter: OnFilter = filterWith && ((kind, field, value) => filterWith({ field, op: kind === 'show' ? '==' : '!=', value }))
  const columnControl = { shown: columns, toggle: (field: string) => changeColumns(toggleColumn(columns, field)) }

  // The inspector steps through the stream's Logs, newest first.
  const streamLogs = result.status === 'done' ? result.stream.logs : []
  const inspectedAt = inspected ? streamLogs.findIndex(l => l.id === inspected.id) : -1
  const onNewer = inspectedAt > 0 ? () => setInspected(streamLogs[inspectedAt - 1]) : null
  const onOlder = inspectedAt >= 0 && inspectedAt < streamLogs.length - 1 ? () => setInspected(streamLogs[inspectedAt + 1]) : null

  // SQL runs only on Run, so in SQL mode a picked range waits for it.
  const pickBarRange = (range: TimeRange) => sqlMode ? setExplorer(pickRange(explorer, range)) : commit(pickRange(explorer, range))

  const closeInspector = useCallback(() => setInspected(null), [])

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
          <RangeSelect range={explorer.draft.range} onChange={pickBarRange} />
          <ModeSwitch modes={modes} mode={mode} onSwitch={switchTo} />
          <div className="h-5 w-px bg-border" />
          {mode === 'builder' ? (
            <BuilderBar view={builderView} />
          ) : (
            <span className="flex-1 text-[11px] text-muted-foreground">Ctrl+Enter runs the {sqlMode ? 'SQL' : 'Query'}.</span>
          )}
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

        {!sqlMode && (
          <HistogramStrip view={histogram} onZoom={range => commit(pickRange(explorer, range))} onLevel={onLevel} />
        )}

        <div className="relative flex-1 min-h-0">
          <TagMenuProvider onFilter={onFilter} columns={columnControl}>
            <ResultArea
              result={result}
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

function ResultArea({ result, onLoadOlder, selectedId, onSelect, columns, onColumns }: {
  result: Result; onLoadOlder: () => void; selectedId: string | null; onSelect: (log: Log) => void
  columns: string[]; onColumns: (columns: string[]) => void
}) {
  switch (result.status) {
    case 'waiting':
      return <Notice>Press Run to run the SQL.</Notice>
    case 'running':
      return <Notice>Running the Query…</Notice>
    case 'cancelled':
      return <Notice>Query cancelled. Run it again to see Logs.</Notice>
    case 'error':
      return <Notice tone="error">{result.message}</Notice>
    case 'table':
      return <LogTableView table={result.table} />
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

function Notice({ children, tone }: { children: React.ReactNode; tone?: 'error' }) {
  if (tone === 'error') {
    // An error keeps its lines, left-aligned as one block, so DuckDB's ^ stays under the mistake it points at.
    return (
      <div className="p-10 text-sm text-destructive">
        <p className="mx-auto w-fit max-w-full whitespace-pre-wrap break-words">{children}</p>
      </div>
    )
  }
  return <div className="p-10 text-center text-sm text-muted-foreground">{children}</div>
}

const CUSTOM = 'custom'

function RangeSelect({ range, onChange }: { range: TimeRange; onChange: (range: TimeRange) => void }) {
  const items: Record<string, string> = Object.fromEntries(RANGE_PRESETS.map(p => [p.span, p.label]))
  let value: string
  if (range.kind === 'relative') {
    value = range.span
    items[value] ??= spanLabel(value)
  } else {
    value = CUSTOM
    items[CUSTOM] = `${format(range.from, 'MMM dd HH:mm')} – ${format(range.to, 'MMM dd HH:mm')}`
  }
  return (
    <Select value={value} onValueChange={v => v && v !== CUSTOM && onChange({ kind: 'relative', span: v as string })} items={items}>
      <SelectTrigger className="h-8 text-xs" aria-label="Time range"><SelectValue /></SelectTrigger>
      {/* Below the trigger, as the Project select opens: lined up with it, the list would cover the bar. */}
      <SelectContent align="start" alignItemWithTrigger={false} className="w-auto min-w-(--anchor-width) max-w-(--available-width)">
        {Object.entries(items).map(([v, label]) => <SelectItem key={v} value={v}>{label}</SelectItem>)}
      </SelectContent>
    </Select>
  )
}
