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
import { Checkbox } from '@/components/ui/checkbox'
import { costText, tokensText, totalTokens, whoText, type AgentSessionRow } from '@/lib/agentSessions'
import {
  fetchRawLine,
  fetchTimelinePage,
  fromText,
  groupTools,
  isThinking,
  messageText,
  rawLineText,
  showItems,
  spanText,
  subagentBrief,
  timelineItems,
  toolCounts,
  toolSummary,
  type RawLine,
  type ShownItem,
  type ToolItem,
  type TimelinePage,
  type TimelineShow,
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

/** What the timelines show besides the person messages and the agent replies. */
const Show = createContext<TimelineShow>({ tools: true, thinking: true })

/** True inside a subagent's own timeline, whose steps need no subagent badge. */
const InSubagent = createContext(false)

const SHOW_KEY = 'solid-trace.agent-session.show'

/** The viewer's last choices, kept in the browser only as a convenience. */
function readShow(): TimelineShow {
  try {
    const saved = JSON.parse(localStorage.getItem(SHOW_KEY) ?? '{}')
    return { tools: saved.tools !== false, thinking: saved.thinking !== false }
  } catch {
    return { tools: true, thinking: true }
  }
}

function saveShow(show: TimelineShow) {
  try {
    localStorage.setItem(SHOW_KEY, JSON.stringify(show))
  } catch {
    // The choices are only a convenience.
  }
}

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
  const [show, setShow] = useState(readShow)
  const change = (next: Partial<TimelineShow>) => {
    const all = { ...show, ...next }
    setShow(all)
    saveShow(all)
  }
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
            ? (
              <Show.Provider value={show}>
                <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-xs" data-section="timeline-show">
                  <Toggle id="show-tools" label="Show tool calls" checked={show.tools} onChange={tools => change({ tools })} />
                  <Toggle id="show-thinking" label="Show agent thinking" checked={show.thinking} onChange={thinking => change({ thinking })} />
                </div>
                <Transcript base={view.timeline_path} subagents={view.subagents} first={{ steps: view.steps, next_cursor: view.next_cursor }} />
              </Show.Provider>
            )
            : <SearchResults key={search} base={view.timeline_path} search={search} />}
        </div>
      )}
    </DashboardLayout>
    </LinePath.Provider>
  )
}

function Toggle({ id, label, checked, onChange }: { id: string; label: string; checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <div className="flex items-center gap-2">
      <Checkbox id={id} checked={checked} onCheckedChange={next => onChange(next === true)} />
      <label htmlFor={id} className="cursor-pointer select-none">{label}</label>
    </div>
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
    ['Estimated cost', <span>{session.cost_usd === null ? <span className="text-muted-foreground">{costText(null)}</span> : <span className="font-mono text-xs">{costText(session.cost_usd)}</span>} <span className="text-xs text-muted-foreground">({costLabel})</span></span>],
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
  const show = useContext(Show)
  const items = showItems(timelineItems(steps, path ? subagents.filter(s => s.tool_use_id) : subagents, cursor === null), show)
  return (
    <div className="space-y-2" data-transcript={path ?? 'main'}>
      {items.length === 0 && <p className="text-sm text-muted-foreground">{steps.length === 0 ? 'No steps.' : 'No steps to show. Turn on tool calls or agent thinking above.'}</p>}
      {groupTools(items).map(item => <Item key={itemKey(item)} item={item} base={base} subagents={subagents} />)}
      {error && <p className="text-sm text-destructive">{error}</p>}
      {cursor !== null && (
        <Button variant="outline" size="sm" onClick={more} disabled={loading}>
          {loading ? 'Loading…' : 'Load more steps'}
        </Button>
      )}
    </div>
  )
}

function itemKey(item: ShownItem): string {
  if (item.kind === 'step') return item.step.id
  if (item.kind === 'subagent') return item.subagent.path
  if (item.kind === 'tools') return `tools-${itemKey(item.tools[0])}`
  return (item.call ?? item.result)!.id
}

function Item({ item, base, subagents }: { item: ShownItem; base: string; subagents: TimelineSubagent[] }) {
  if (item.kind === 'subagent') return <Subagent subagent={item.subagent} base={base} subagents={subagents} />
  if (item.kind === 'step') return <Message step={item.step} />
  if (item.kind === 'tools') return <ToolGroup tools={item.tools} base={base} subagents={subagents} />
  return <ToolCall tool={item} base={base} subagents={subagents} />
}

/** The classes of the one-line button that opens a tool call or a group. */
const FOLD = 'flex w-full min-w-0 items-center gap-2 px-3 py-1 text-left text-xs text-muted-foreground outline-none hover:bg-muted/40 hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring'

function Fold({ open }: { open: boolean }) {
  return open
    ? <ChevronDown className="h-3.5 w-3.5 shrink-0" aria-hidden />
    : <ChevronRight className="h-3.5 w-3.5 shrink-0" aria-hidden />
}

/**
 * A run of tool calls as one line that tells how many calls of each tool
 * it has. It opens to show each call as one line.
 */
function ToolGroup({ tools, base, subagents }: { tools: ToolItem[]; base: string; subagents: TimelineSubagent[] }) {
  const [open, setOpen] = useState(false)
  const first = (tools[0].call ?? tools[0].result)!
  const lastTool = tools[tools.length - 1]
  const last = (lastTool.result ?? lastTool.call)!
  const failed = tools.filter(t => t.result?.level === 'error').length
  return (
    <div data-tool-group className={open ? 'border' : undefined}>
      <button type="button" className={FOLD} onClick={() => setOpen(!open)} aria-expanded={open}>
        <Fold open={open} />
        <Wrench className="h-3.5 w-3.5 shrink-0" aria-hidden />
        <span className="shrink-0 font-medium text-foreground">{tools.length} tool calls</span>
        <span className="min-w-0 flex-1 truncate">{toolCounts(tools)}</span>
        {failed > 0 && <Badge variant="error">{failed} failed</Badge>}
        <span className="shrink-0 font-mono max-sm:hidden">{clock(first.time)}–{clock(last.time)}</span>
      </button>
      {open && (
        <div className="ml-4 border-l pb-1">
          {tools.map(tool => <ToolCall key={itemKey(tool)} tool={tool} base={base} subagents={subagents} />)}
        </div>
      )}
    </div>
  )
}

/**
 * A tool call as one line: the tool and what it did. It opens to show the
 * input and the output. A subagent that the call started always shows.
 */
function ToolCall({ tool, base, subagents }: { tool: ToolItem; base: string; subagents: TimelineSubagent[] }) {
  const [open, setOpen] = useState(false)
  const { call, result, subagent } = tool
  if (subagent) {
    // The call only starts the subagent, and its output is Claude Code's
    // note to the main agent, so the subagent is what shows.
    return (
      <div data-step="tool" data-tool={(call ?? result)!.tool_name}>
        <Subagent subagent={subagent} base={base} subagents={subagents} started={tool} />
      </div>
    )
  }
  const step = (call ?? result)!
  const failed = result?.level === 'error'
  const summary = toolSummary(call)
  return (
    <div data-step="tool" data-tool={step.tool_name} className={open ? 'border' : undefined}>
      <button type="button" className={FOLD} onClick={() => setOpen(!open)} aria-expanded={open}>
        <Fold open={open} />
        <Wrench className="h-3.5 w-3.5 shrink-0" aria-hidden />
        <span className="shrink-0 font-medium text-foreground">{step.tool_name || 'Tool'}</span>
        <span className="min-w-0 flex-1 truncate">{summary}</span>
        {failed && <Badge variant="error">error</Badge>}
        {!result && <span className="shrink-0">no output yet</span>}
        <span className="shrink-0 font-mono max-sm:hidden">{clock(step.time)}</span>
        {step.tokens && <span className="shrink-0 font-mono max-md:hidden">{stepTokensText(step)}</span>}
      </button>
      {open && (
        <div className="space-y-2 px-3 pb-3">
          {call && <Value label="Input" step={call} />}
          {result && <Value label="Output" step={result} error={failed} />}
          {!result && <p className="text-xs text-muted-foreground">No output yet.</p>}
        </div>
      )}
    </div>
  )
}

/**
 * One message, weighted by whom it is from, so that a person who scrolls
 * sees the conversation first: the person's messages are the strongest,
 * the agent's replies are cards, its thinking is quiet text, and the text
 * that Claude Code adds (commands, skills, reminders) is quiet and folds.
 */
function Message({ step }: { step: TimelineStep }) {
  const inSubagent = useContext(InSubagent)
  const kind = step.from.kind
  const text = messageText(step.text)
  if (!text) return null
  const title = (
    <StepTitle step={step} label={fromText(step.from)} subagentBadge={!inSubagent}
      icon={kind === 'agent' ? <Bot className="h-3.5 w-3.5" aria-hidden /> : null} />
  )
  if (kind === 'person') {
    return (
      <div className="border border-l-4 border-primary/40 border-l-primary bg-primary/10" data-step={kind}>
        {title}
        <div className="px-3 pb-3"><Text step={step} text={text} /></div>
      </div>
    )
  }
  if (kind === 'agent' && isThinking(step)) {
    return (
      <div className="ml-3 border-l-2 border-muted-foreground/30 text-muted-foreground" data-step={kind} data-thinking>
        {title}
        <div className="px-3 pb-1"><Text step={step} text={text} /></div>
      </div>
    )
  }
  if (kind === 'agent') {
    return (
      <div className="border" data-step={kind}>
        {title}
        <div className="px-3 pb-3"><Text step={step} text={text} /></div>
      </div>
    )
  }
  return (
    <div className="border border-dashed text-muted-foreground" data-step={kind}>
      {title}
      <div className="px-3 pb-2"><Text step={step} text={text} short={300} /></div>
    </div>
  )
}

function StepTitle({ step, label, icon, subagentBadge = true }: { step: TimelineStep; label: string; icon: React.ReactNode; subagentBadge?: boolean }) {
  return (
    <div className="flex flex-wrap items-center gap-2 px-3 py-1.5 text-xs">
      {icon}
      <span className="font-medium">{label}</span>
      {isThinking(step) && <Badge>thinking</Badge>}
      {subagentBadge && step.path.includes('subagents/') && <Badge>subagent {step.path.split('/').pop()?.replace(/^agent-|\.jsonl$/g, '')}</Badge>}
      <span className="font-mono text-muted-foreground">{clock(step.time)}</span>
      {step.tokens && (
        <span className="font-mono text-muted-foreground" title={`${step.tokens.input} input, ${step.tokens.output} output, ${step.tokens.cache_read} cache read, ${step.tokens.cache_write} cache write`}>
          {stepTokensText(step)}
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
function Text({ step, mono = false, text: shownText, short = SHORT }: { step: TimelineStep; mono?: boolean; text?: string; short?: number }) {
  const linePath = useContext(LinePath)
  const [open, setOpen] = useState(false)
  const [full, setFull] = useState<RawLine | null>(null)
  const [showFull, setShowFull] = useState(false)
  const [loadingFull, setLoadingFull] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const body = shownText ?? step.text
  const long = body.length > short
  const text = long && !open ? `${body.slice(0, short)}…` : body
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

/**
 * A subagent inside its parent, named by the description that the main
 * agent gave it. Its steps load when a member opens it; the tool call that
 * started it is one folded line above them.
 */
function Subagent({ subagent, base, subagents, started }: {
  subagent: TimelineSubagent
  base: string
  subagents: TimelineSubagent[]
  started?: ToolItem
}) {
  const brief = subagentBrief(started?.call ?? null)
  const show = useContext(Show)
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
      <button
        type="button"
        className="flex w-full min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 py-1 text-left text-xs outline-none hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
        onClick={toggle}
        aria-expanded={open}
        title={`Subagent ${subagent.agent_id}`}
      >
        <Fold open={open} />
        <Bot className="h-3.5 w-3.5 shrink-0" aria-hidden />
        <span className="font-medium">Subagent{brief.description ? `: ${brief.description}` : ` ${subagent.agent_id}`}</span>
        <span className="font-mono text-muted-foreground">
          {brief.type && `${brief.type}, `}{clock(subagent.first)}–{clock(subagent.last)}, {spanText(subagent.first, subagent.last)}
        </span>
      </button>
      {open && started && show.tools && <ToolCall tool={{ ...started, subagent: null }} base={base} subagents={subagents} />}
      {open && error && <p className="text-sm text-destructive">{error}</p>}
      {open && !error && !first && <p className="text-xs text-muted-foreground">Loading…</p>}
      {open && first && (
        <InSubagent.Provider value>
          <Transcript base={base} path={subagent.path} subagents={subagents} first={first} />
        </InSubagent.Provider>
      )}
    </div>
  )
}

function stepTokensText(step: TimelineStep): string {
  const t = step.tokens!
  return `${tokensText(t.input + t.output + t.cache_read + t.cache_write)} tokens · ${costText(step.cost_usd)}`
}

function clock(time: string): string {
  return format(new Date(time), 'HH:mm:ss')
}

function timeText(time: string): string {
  return format(new Date(time), 'MMM d, HH:mm:ss')
}
