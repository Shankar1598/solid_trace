import { Link } from '@inertiajs/react'
import { format } from 'date-fns'
import { AlertTriangle, Bot, ChevronDown, ChevronRight, Wrench } from 'lucide-react'
import { createContext, useContext, useEffect, useState } from 'react'
import { AgentSessionSearch } from '@/components/AgentSessionSearch'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { costText, tokensText, totalTokens, whoText, type AgentSessionRow } from '@/lib/agentSessions'
import {
  fetchRawLine,
  fetchTimelinePage,
  fromText,
  rawLineText,
  timelineItems,
  type RawLine,
  type TimelineItem,
  type TimelinePage,
  type TimelineStep,
  type TimelineSubagent,
} from '@/lib/agentSessionTimeline'

interface AgentSessionProps {
  event_store: { available: boolean; error?: string }
  agent_session: AgentSessionRow | null
  subagents: TimelineSubagent[]
  steps: TimelineStep[]
  next_cursor: string | null
  cost_label: string
  resume_warning: string | null
  list_path: string
  timeline_path: string
  line_path: string
  parent_path: string | null
}

/** The URL of the raw lines of this Agent session, for 'Show full'. */
const LinePath = createContext('')

/**
 * The Agent session page: the header of one Agent session and its
 * timeline of person messages, agent replies and tool calls, with each
 * subagent inside the tool call that started it. A long timeline loads in
 * pages. A search shows the steps of this Agent session, its subagents'
 * too, that have the text; 'Show full' on a cut value reads its raw line.
 */
export default function AgentSessionShow({ agent_session: view }: { agent_session: AgentSessionProps }) {
  const session = view.agent_session
  const [search, setSearch] = useState<string | null>(null)
  return (
    <LinePath.Provider value={view.line_path}>
    <DashboardLayout>
      <PageHeader
        breadcrumbs={[{ label: 'Agent sessions', href: view.list_path }, { label: session ? whoText(session) : 'Agent session' }]}
        title={session ? whoText(session) : 'Agent session'}
        tabTitle="Agent session"
        badge={session?.flags.map(flag => <Badge key={flag} variant="warn">{flag}</Badge>)}
        description={session && <span className="font-mono text-xs">{session.id}</span>}
      />
      {!view.event_store.available && (
        <Card className="border-destructive/40" data-notice="event-store-error">
          <CardContent className="flex items-start gap-2 py-4 text-sm text-destructive">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
            {view.event_store.error}
          </CardContent>
        </Card>
      )}
      {session && (
        <div className="space-y-4">
          {view.resume_warning && (
            <div className="flex items-start gap-2 border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-warning" data-notice="resume-warning">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
              {view.resume_warning}
            </div>
          )}
          <Header session={session} costLabel={view.cost_label} parentPath={view.parent_path} />
          <AgentSessionSearch
            label="Search this Agent session"
            placeholder="Search the steps of this Agent session…"
            onSearch={setSearch}
            onClear={() => setSearch(null)}
            active={search !== null}
          />
          {search === null
            ? <Transcript base={view.timeline_path} subagents={view.subagents} first={{ steps: view.steps, next_cursor: view.next_cursor }} />
            : <SearchResults key={search} base={view.timeline_path} search={search} />}
        </div>
      )}
    </DashboardLayout>
    </LinePath.Provider>
  )
}

/** The steps of the Agent session, its subagents' too, that have the text, in pages. */
function SearchResults({ base, search }: { base: string; search: string }) {
  const [steps, setSteps] = useState<TimelineStep[] | null>(null)
  const [cursor, setCursor] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const load = async (after: string | null) => {
    setLoading(true)
    setError(null)
    try {
      const page = await fetchTimelinePage(base, { search, cursor: after })
      setSteps(current => [...(after ? current ?? [] : []), ...page.steps])
      setCursor(page.next_cursor)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }
  // The first page once: a new search makes a new SearchResults (its key).
  useEffect(() => { void load(null) }, [])
  return (
    <div className="space-y-2" data-section="search-results">
      {steps === null && !error && <p className="text-xs text-muted-foreground">Searching…</p>}
      {steps?.length === 0 && <p className="text-sm text-muted-foreground">No steps have “{search}”.</p>}
      {steps?.map(step => <Message key={step.id} step={step} />)}
      {error && <p className="text-sm text-destructive">{error}</p>}
      {cursor !== null && (
        <Button variant="outline" size="sm" onClick={() => load(cursor)} disabled={loading}>
          {loading ? 'Loading…' : 'Load more steps'}
        </Button>
      )}
    </div>
  )
}

function Header({ session, costLabel, parentPath }: { session: AgentSessionRow; costLabel: string; parentPath: string | null }) {
  const facts: [string, React.ReactNode][] = [
    [session.run ? 'Run' : 'Person', whoText(session)],
    ['Repository', <span className="font-mono text-xs">{session.repository}</span>],
    ['Folder', session.folder ? <span className="font-mono text-xs">{session.folder}</span> : <None />],
    ['Parent Agent session', parentPath
      ? <Link href={parentPath} className="font-mono text-xs underline underline-offset-2">{session.parent_session_id}</Link>
      : <None />],
    ['Model', session.model ? <span className="font-mono text-xs">{session.model}</span> : <None text="Not known" />],
    ['First', timeText(session.first)],
    ['Last', timeText(session.last)],
    ['Tokens', totalTokens(session) === 0 ? <None text="No tokens" /> : (
      <span className="font-mono text-xs tabular-nums">
        {tokensText(totalTokens(session))}: {session.input_tokens.toLocaleString()} input, {session.output_tokens.toLocaleString()} output,{' '}
        {session.cache_read_tokens.toLocaleString()} cache read, {session.cache_write_tokens.toLocaleString()} cache write
      </span>
    )],
    ['Estimated cost', <span><span className="font-mono text-xs">{costText(session.cost_usd)}</span> <span className="text-xs text-muted-foreground">({costLabel})</span></span>],
    ['Flags', session.flags.length ? session.flags.join(', ') : <None />],
  ]
  return (
    <Card data-section="agent-session-header">
      <CardContent className="grid gap-x-6 gap-y-2 py-4 text-sm sm:grid-cols-2 lg:grid-cols-3">
        {facts.map(([label, value]) => (
          <div key={label} className="min-w-0">
            <div className="text-xs text-muted-foreground">{label}</div>
            <div className="break-words">{value}</div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function None({ text = 'None' }: { text?: string }) {
  return <span className="text-muted-foreground">{text}</span>
}

/** One transcript's timeline, the main one or a subagent's, with a button for its next page. */
function Transcript({ base, path, subagents, first }: { base: string; path?: string; subagents: TimelineSubagent[]; first: TimelinePage }) {
  const [steps, setSteps] = useState(first.steps)
  const [cursor, setCursor] = useState(first.next_cursor)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const more = async () => {
    setLoading(true)
    setError(null)
    try {
      const page = await fetchTimelinePage(base, { path, cursor })
      setSteps(current => [...current, ...page.steps])
      setCursor(page.next_cursor)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }
  // A subagent's own subagents show inside it, so the main timeline shows
  // only those that no call names.
  const items = timelineItems(steps, path ? subagents.filter(s => s.tool_use_id) : subagents, cursor === null)
  return (
    <div className="space-y-2" data-transcript={path ?? 'main'}>
      {items.length === 0 && <p className="text-sm text-muted-foreground">No steps.</p>}
      {items.map(item => <Item key={itemKey(item)} item={item} base={base} subagents={subagents} />)}
      {error && <p className="text-sm text-destructive">{error}</p>}
      {cursor !== null && (
        <Button variant="outline" size="sm" onClick={more} disabled={loading}>
          {loading ? 'Loading…' : 'Load more steps'}
        </Button>
      )}
    </div>
  )
}

function itemKey(item: TimelineItem): string {
  if (item.kind === 'step') return item.step.id
  if (item.kind === 'subagent') return item.subagent.path
  return (item.call ?? item.result)!.id
}

function Item({ item, base, subagents }: { item: TimelineItem; base: string; subagents: TimelineSubagent[] }) {
  if (item.kind === 'subagent') return <Subagent subagent={item.subagent} base={base} subagents={subagents} />
  if (item.kind === 'step') return <Message step={item.step} />
  const { call, result, subagent } = item
  const step = (call ?? result)!
  return (
    <div className="border" data-step="tool" data-tool={step.tool_name}>
      <StepTitle step={step} icon={<Wrench className="h-3.5 w-3.5" aria-hidden />} label={step.tool_name || 'Tool'} />
      <div className="space-y-2 px-3 pb-3">
        {call && <Value label="Input" step={call} />}
        {result && <Value label="Output" step={result} error={result.level === 'error'} />}
        {!result && <p className="text-xs text-muted-foreground">No output yet.</p>}
        {subagent && <Subagent subagent={subagent} base={base} subagents={subagents} />}
      </div>
    </div>
  )
}

function Message({ step }: { step: TimelineStep }) {
  const person = step.from.kind === 'person'
  return (
    <div className={person ? 'border border-primary/30 bg-primary/5' : 'border'} data-step={step.from.kind}>
      <StepTitle step={step} label={fromText(step.from)} icon={step.from.kind === 'agent' ? <Bot className="h-3.5 w-3.5" aria-hidden /> : null} />
      <div className="px-3 pb-3"><Text step={step} /></div>
    </div>
  )
}

function StepTitle({ step, label, icon }: { step: TimelineStep; label: string; icon: React.ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-2 px-3 py-1.5 text-xs">
      {icon}
      <span className="font-medium">{label}</span>
      {step.line.endsWith('/thinking') && <Badge>thinking</Badge>}
      {step.path.includes('subagents/') && <Badge>subagent {step.path.split('/').pop()?.replace(/^agent-|\.jsonl$/g, '')}</Badge>}
      <span className="font-mono text-muted-foreground">{format(new Date(step.time), 'HH:mm:ss')}</span>
      {step.tokens && (
        <span className="font-mono text-muted-foreground" title={`${step.tokens.input} input, ${step.tokens.output} output, ${step.tokens.cache_read} cache read, ${step.tokens.cache_write} cache write`}>
          {tokensText(step.tokens.input + step.tokens.output + step.tokens.cache_read + step.tokens.cache_write)} tokens · {costText(step.cost_usd)}
        </span>
      )}
    </div>
  )
}

function Value({ label, step, error = false }: { label: string; step: TimelineStep; error?: boolean }) {
  return (
    <div>
      <div className="mb-1 flex items-center gap-2 text-xs text-muted-foreground">{label}{error && <Badge variant="error">error</Badge>}</div>
      <Text step={step} mono />
    </div>
  )
}

const SHORT = 1200

/**
 * A step's text: a long one opens on request, and a cut one says so and
 * gives 'Show full', which reads the step's raw line.
 */
function Text({ step, mono = false }: { step: TimelineStep; mono?: boolean }) {
  const linePath = useContext(LinePath)
  const [open, setOpen] = useState(false)
  const [full, setFull] = useState<RawLine | null>(null)
  const [showFull, setShowFull] = useState(false)
  const [loadingFull, setLoadingFull] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const long = step.text.length > SHORT
  const text = long && !open ? `${step.text.slice(0, SHORT)}…` : step.text
  const toggleFull = async () => {
    if (showFull || full) {
      setShowFull(!showFull)
      return
    }
    setLoadingFull(true)
    setError(null)
    try {
      setFull(await fetchRawLine(linePath, step))
      setShowFull(true)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoadingFull(false)
    }
  }
  return (
    <div>
      {showFull && full
        ? (
          <div data-raw-line>
            <div className="mb-1 text-xs text-muted-foreground">
              The raw line, redacted{full.cut ? '. The record does not hold all of it.' : '.'}
            </div>
            <pre className="max-h-[60vh] max-w-full overflow-auto whitespace-pre-wrap break-words bg-muted/40 p-2 font-mono text-xs">{rawLineText(full.line)}</pre>
          </div>
        )
        : <pre className={`max-w-full whitespace-pre-wrap break-words text-sm ${mono ? 'bg-muted/40 p-2 font-mono text-xs' : 'font-sans'}`}>{text}</pre>}
      <div className="mt-1 flex flex-wrap items-center gap-2">
        {long && !showFull && (
          <button type="button" className="text-xs underline underline-offset-2" onClick={() => setOpen(!open)}>
            {open ? 'Show less' : 'Show more'}
          </button>
        )}
        {step.cut && <Badge variant="warn" title="The text was cut at the Log limits. The raw line has all of it.">cut</Badge>}
        {step.cut && (
          <button type="button" className="text-xs underline underline-offset-2" onClick={toggleFull} disabled={loadingFull}>
            {loadingFull ? 'Loading…' : showFull ? 'Show the step' : 'Show full'}
          </button>
        )}
        {error && <span className="text-xs text-destructive">{error}</span>}
      </div>
    </div>
  )
}

/** A subagent inside its parent: its steps load when a member opens it. */
function Subagent({ subagent, base, subagents }: { subagent: TimelineSubagent; base: string; subagents: TimelineSubagent[] }) {
  const [first, setFirst] = useState<TimelinePage | null>(null)
  const [open, setOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const toggle = async () => {
    setOpen(!open)
    if (first || open) return
    try {
      setFirst(await fetchTimelinePage(base, { path: subagent.path }))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }
  return (
    <div className="border-l-2 border-primary/40 pl-3" data-subagent={subagent.agent_id}>
      <button type="button" className="flex items-center gap-1 py-1 text-xs font-medium" onClick={toggle} aria-expanded={open}>
        {open ? <ChevronDown className="h-3.5 w-3.5" aria-hidden /> : <ChevronRight className="h-3.5 w-3.5" aria-hidden />}
        Subagent {subagent.agent_id}
        <span className="font-mono font-normal text-muted-foreground">{format(new Date(subagent.first), 'HH:mm:ss')}–{format(new Date(subagent.last), 'HH:mm:ss')}</span>
      </button>
      {open && error && <p className="text-sm text-destructive">{error}</p>}
      {open && !error && !first && <p className="text-xs text-muted-foreground">Loading…</p>}
      {open && first && <Transcript base={base} path={subagent.path} subagents={subagents} first={first} />}
    </div>
  )
}

function timeText(time: string): string {
  return format(new Date(time), 'MMM d, HH:mm:ss')
}
