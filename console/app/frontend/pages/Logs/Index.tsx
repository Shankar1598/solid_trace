import { useCallback, useEffect, useRef, useState } from 'react'
import { router, usePage } from '@inertiajs/react'
import { format } from 'date-fns'
import { Play, Square } from 'lucide-react'
import { toast } from 'sonner'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { RunError, fetchHistogram, fetchLogPage, fetchRun, validateQuery, type QueryProblem } from '@/lib/logs/api'
import {
  EMPTY_BUILDER,
  builderOf,
  flipFilter,
  hide,
  queryOf,
  removeFilter,
  search,
  showOnly,
  type Builder,
  type Chip,
} from '@/lib/logs/builder'
import {
  MODES,
  RANGE_PRESETS,
  openExplorer,
  pickRange,
  resolveRange,
  runExplorer,
  searchOf,
  spanLabel,
  switchMode,
  type Explorer,
  type ExplorerState,
  type Mode,
  type TimeRange,
  typeQuery,
} from '@/lib/logs/explorerState'
import { bucketSeconds, filterByLevel, type HistogramLevel } from '@/lib/logs/histogram'
import { pivotQuery, toBuilder } from '@/lib/logs/queryMode'
import { addOlderPage, startStream, streamEnd, type Log, type Stream } from '@/lib/logs/stream'
import type { LogTable } from '@/lib/logs/table'
import { SharedProps } from '@/types'
import { BuilderBar, type BuilderView } from './components/BuilderBar'
import { HistogramStrip, type HistogramView } from './components/HistogramStrip'
import { LogInspector } from './components/LogInspector'
import { LogStream } from './components/LogStream'
import { LogTableView } from './components/LogTableView'
import { ModeSwitch } from './components/ModeSwitch'
import { QueryBox, type LeavingQuery } from './components/QueryBox'
import { TagMenuProvider, type OnFilter } from './components/TagMenu'

interface ProjectOption {
  id: number
  name: string
  slug: string
}

interface LogsIndexProps {
  project: ProjectOption
  projects: ProjectOption[]
}

type Result =
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

export default function LogsIndex({ project, projects }: LogsIndexProps) {
  const orgSlug = usePage<SharedProps>().props.current_org?.slug ?? ''
  const basePath = `/${orgSlug}/projects/${project.slug}/logs`

  // Back remounts the page (Inertia restores it without preserving state), so
  // reading the URL once here is enough to undo the last run.
  const [explorer, setExplorer] = useState<Explorer>(() => openExplorer(window.location.search))
  // For answers that arrive after a render: what the explorer is by then.
  const latest = useRef(explorer)
  latest.current = explorer
  const [result, setResult] = useState<Result>({ status: 'running' })
  const [histogram, setHistogram] = useState<HistogramView>({ status: 'running' })
  const [builderView, setBuilderView] = useState<BuilderView>(() =>
    explorer.current.query === '' ? { status: 'ready', builder: EMPTY_BUILDER } : { status: 'reading' })
  const [inspected, setInspected] = useState<Log | null>(null)
  // Query mode: the mistakes of the Query last run, and the prompt to leave.
  const [problems, setProblems] = useState<QueryProblem[]>([])
  const [leaving, setLeaving] = useState<LeavingQuery | null>(null)
  const currentRun = useRef<Run | null>(null)

  const start = useCallback((state: ExplorerState) => {
    currentRun.current?.controller.abort()
    const run: Run = { controller: new AbortController(), query: state.query, ...resolveRange(state.range, new Date()) }
    currentRun.current = run
    setResult({ status: 'running' })
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
        setResult(answer.kind === 'table'
          ? { status: 'table', table: answer.table }
          : { status: 'done', stream: startStream(answer.page), loadingOlder: false, olderError: null })
      })
      .catch((error: Error) => {
        if (currentRun.current !== run || run.controller.signal.aborted) return
        setResult({ status: 'error', message: error.message })
        if (error instanceof RunError && error.problems.length > 0) setProblems(error.problems)
      })
  }, [orgSlug, project.slug])

  useEffect(() => {
    start(explorer.current)
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

  const replaceUrl = (search: string) => router.replace({ url: basePath + search, preserveState: true, preserveScroll: true })

  const run = (next: Explorer) => {
    const { explorer: ran, push } = runExplorer(next)
    setExplorer(ran)
    if (push) router.push({ url: basePath + push, preserveState: true, preserveScroll: true })
    start(ran.current)
  }

  /**
   * Runs the draft. In Query mode a Query with mistakes doesn't run: they
   * show under the Query box, and the last good result stays.
   */
  const commit = (next: Explorer) => {
    setLeaving(null)
    if (next.draft.mode !== 'query') return run(next)
    validateQuery({ orgSlug, projectSlug: project.slug, query: next.draft.query })
      .then(({ errors }) => {
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
      commit(typeQuery(next, queryOf(builder)))
    } else {
      setExplorer(next)
      replaceUrl(replace)
    }
  }

  /**
   * Builder → Query keeps the Builder's Query as text. Query → Builder is
   * silent when the Builder can show the text; otherwise it asks first.
   */
  const switchTo = (mode: Mode) => {
    if (mode === 'query') {
      const { explorer: next, replace } = switchMode(explorer, 'query')
      setExplorer(next)
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

  /** Discard and switch: back to the Builder's last filters. */
  const discardQuery = () => {
    const builder = builderView.status === 'ready' ? builderView.builder : EMPTY_BUILDER
    enterBuilder(builder, queryOf(builder) !== explorer.current.query)
  }

  /** A pivot in Query mode: a where after the leading filters of the Query that ran. */
  const pivot = (chip: Chip) => {
    const query = explorer.current.query
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
    const next = typeQuery(explorer, queryOf(builder))
    if (run) commit(next)
    else setExplorer(next)
  }

  const mode = explorer.current.mode
  const builder = mode === 'builder' && builderView.status === 'ready' ? builderView.builder : null
  let onFilter: OnFilter = null
  let onLevel: ((level: HistogramLevel) => void) | null = null
  if (builder) {
    onFilter = (kind, field, value) =>
      changeBuilder(kind === 'show' ? showOnly(builder, field, value) : hide(builder, field, value), true)
    onLevel = level => {
      const filtered = filterByLevel(builder, level)
      if (filtered) changeBuilder(filtered, true)
    }
  } else if (mode === 'query') {
    onFilter = (kind, field, value) => pivot({ field, op: kind === 'show' ? '==' : '!=', value })
    onLevel = level => {
      if (level !== 'none') pivot({ field: 'level', op: '==', value: level })
    }
  }

  const closeInspector = useCallback(() => setInspected(null), [])

  const cancel = () => {
    currentRun.current?.controller.abort()
    setResult({ status: 'cancelled' })
    setHistogram(h => h.status === 'running' ? { status: 'cancelled' } : h)
  }

  const loadOlder = useCallback(() => {
    const run = currentRun.current
    if (!run || result.status !== 'done' || result.loadingOlder) return
    const cursor = result.stream.nextCursor
    setResult({ ...result, loadingOlder: true, olderError: null })

    fetchLogPage({ orgSlug, projectSlug: project.slug, query: run.query, from: run.from, to: run.to, cursor, signal: run.controller.signal })
      .then(page => {
        if (currentRun.current !== run) return
        setResult(r => r.status === 'done' ? { ...r, stream: addOlderPage(r.stream, page), loadingOlder: false } : r)
      })
      .catch((error: Error) => {
        if (currentRun.current !== run || run.controller.signal.aborted) return
        setResult(r => r.status === 'done' ? { ...r, loadingOlder: false, olderError: error.message } : r)
      })
  }, [orgSlug, project.slug, result])

  const openProject = (slug: string) => {
    if (slug !== project.slug) router.get(`/${orgSlug}/projects/${slug}/logs${searchOf(explorer.current)}`)
  }

  const running = result.status === 'running'

  return (
    <DashboardLayout>
      <div className="h-[calc(100vh-2rem)] flex flex-col rounded-md border overflow-hidden bg-card">
        <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
          <ProjectSelect project={project} projects={projects} onChange={openProject} />
          <RangeSelect range={explorer.draft.range} onChange={range => commit(pickRange(explorer, range))} />
          <ModeSwitch modes={MODES} mode={mode} onSwitch={switchTo} />
          <div className="h-5 w-px bg-border" />
          {mode === 'builder' ? (
            <BuilderBar
              view={builderView}
              onFlip={i => builder && changeBuilder(flipFilter(builder, i), true)}
              onRemove={i => builder && changeBuilder(removeFilter(builder, i), true)}
              onSearch={text => builder && changeBuilder(search(builder, text), false)}
              onRun={() => commit(explorer)}
            />
          ) : (
            <span className="flex-1 text-[11px] text-muted-foreground">Ctrl+Enter runs the Query.</span>
          )}
          {running ? (
            <Button variant="outline" size="sm" onClick={cancel}><Square className="h-3.5 w-3.5" /> Cancel</Button>
          ) : (
            <Button variant="outline" size="sm" onClick={() => commit(explorer)}><Play className="h-3.5 w-3.5" /> Run</Button>
          )}
        </div>

        {mode === 'query' && (
          <QueryBox
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

        <HistogramStrip view={histogram} onZoom={range => commit(pickRange(explorer, range))} onLevel={onLevel} />

        <div className="relative flex-1 min-h-0">
          <TagMenuProvider onFilter={onFilter}>
            <ResultArea result={result} onLoadOlder={loadOlder} selectedId={inspected?.id ?? null} onSelect={setInspected} />
            {inspected && (
              <LogInspector key={inspected.id} log={inspected} orgSlug={orgSlug} projectSlug={project.slug} onClose={closeInspector} />
            )}
          </TagMenuProvider>
        </div>
      </div>
    </DashboardLayout>
  )
}

function ResultArea({ result, onLoadOlder, selectedId, onSelect }: {
  result: Result; onLoadOlder: () => void; selectedId: string | null; onSelect: (log: Log) => void
}) {
  switch (result.status) {
    case 'running':
      return <Notice>Running the Query…</Notice>
    case 'cancelled':
      return <Notice>Query cancelled. Run it again to see Logs.</Notice>
    case 'error':
      return <Notice tone="error">{result.message}</Notice>
    case 'table':
      return <LogTableView table={result.table} />
    case 'done':
      if (result.stream.logs.length === 0) return <Notice>No Logs match. Widen the time range or change the Query.</Notice>
      return (
        <LogStream
          logs={result.stream.logs}
          end={streamEnd(result.stream)}
          loadingOlder={result.loadingOlder}
          olderError={result.olderError}
          onLoadOlder={onLoadOlder}
          selectedId={selectedId}
          onSelect={onSelect}
        />
      )
  }
}

function Notice({ children, tone }: { children: React.ReactNode; tone?: 'error' }) {
  return (
    <div className={tone === 'error' ? 'p-10 text-center text-sm text-destructive' : 'p-10 text-center text-sm text-muted-foreground'}>
      {children}
    </div>
  )
}

function ProjectSelect({ project, projects, onChange }: { project: ProjectOption; projects: ProjectOption[]; onChange: (slug: string) => void }) {
  const items = Object.fromEntries(projects.map(p => [p.slug, p.name]))
  return (
    <Select value={project.slug} onValueChange={v => v && onChange(v as string)} items={items}>
      <SelectTrigger className="h-8 text-xs" aria-label="Project"><SelectValue /></SelectTrigger>
      <SelectContent>
        {projects.map(p => <SelectItem key={p.slug} value={p.slug}>{p.name}</SelectItem>)}
      </SelectContent>
    </Select>
  )
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
      <SelectContent>
        {Object.entries(items).map(([v, label]) => <SelectItem key={v} value={v}>{label}</SelectItem>)}
      </SelectContent>
    </Select>
  )
}
