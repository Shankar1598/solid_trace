import { useCallback, useEffect, useRef, useState } from 'react'
import { router, usePage } from '@inertiajs/react'
import { format } from 'date-fns'
import { Play, Square } from 'lucide-react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { fetchLogPage } from '@/lib/logs/api'
import {
  RANGE_PRESETS,
  openExplorer,
  pickRange,
  resolveRange,
  runExplorer,
  searchOf,
  spanLabel,
  type Explorer,
  type ExplorerState,
  type TimeRange,
} from '@/lib/logs/explorerState'
import { addOlderPage, startStream, streamEnd, type Stream } from '@/lib/logs/stream'
import { SharedProps } from '@/types'
import { LogStream } from './components/LogStream'

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
  const [result, setResult] = useState<Result>({ status: 'running' })
  const currentRun = useRef<Run | null>(null)

  const start = useCallback((state: ExplorerState) => {
    currentRun.current?.controller.abort()
    const run: Run = { controller: new AbortController(), query: state.query, ...resolveRange(state.range, new Date()) }
    currentRun.current = run
    setResult({ status: 'running' })

    fetchLogPage({ orgSlug, projectSlug: project.slug, query: run.query, from: run.from, to: run.to, signal: run.controller.signal })
      .then(page => {
        if (currentRun.current === run) setResult({ status: 'done', stream: startStream(page), loadingOlder: false, olderError: null })
      })
      .catch((error: Error) => {
        if (currentRun.current === run && !run.controller.signal.aborted) setResult({ status: 'error', message: error.message })
      })
  }, [orgSlug, project.slug])

  useEffect(() => {
    start(explorer.current)
    return () => currentRun.current?.controller.abort()
    // Runs once per mount: later runs start from the bar.
  }, [])

  const commit = (next: Explorer) => {
    const { explorer: ran, push } = runExplorer(next)
    setExplorer(ran)
    if (push) router.push({ url: basePath + push, preserveState: true, preserveScroll: true })
    start(ran.current)
  }

  const cancel = () => {
    currentRun.current?.controller.abort()
    setResult({ status: 'cancelled' })
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
          {explorer.current.query !== '' && (
            <code className="truncate max-w-[50%] rounded bg-muted px-2 py-1 text-[11px] text-muted-foreground" title={explorer.current.query}>
              {explorer.current.query}
            </code>
          )}
          <div className="flex-1" />
          {running ? (
            <Button variant="outline" size="sm" onClick={cancel}><Square className="h-3.5 w-3.5" /> Cancel</Button>
          ) : (
            <Button variant="outline" size="sm" onClick={() => commit(explorer)}><Play className="h-3.5 w-3.5" /> Run</Button>
          )}
        </div>

        <div className="flex-1 min-h-0">
          <ResultArea result={result} onLoadOlder={loadOlder} />
        </div>
      </div>
    </DashboardLayout>
  )
}

function ResultArea({ result, onLoadOlder }: { result: Result; onLoadOlder: () => void }) {
  switch (result.status) {
    case 'running':
      return <Notice>Running the Query…</Notice>
    case 'cancelled':
      return <Notice>Query cancelled. Run it again to see Logs.</Notice>
    case 'error':
      return <Notice tone="error">{result.message}</Notice>
    case 'done':
      if (result.stream.logs.length === 0) return <Notice>No Logs match. Widen the time range or change the Query.</Notice>
      return (
        <LogStream
          logs={result.stream.logs}
          end={streamEnd(result.stream)}
          loadingOlder={result.loadingOlder}
          olderError={result.olderError}
          onLoadOlder={onLoadOlder}
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
