import { Link, router, usePage } from '@inertiajs/react'
import { format } from 'date-fns'
import { AlertTriangle, Bot } from 'lucide-react'
import { AgentSessionSearch } from '@/components/AgentSessionSearch'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { TimeRangePicker } from '@/components/TimeRangePicker'
import { Badge } from '@/components/ui/badge'
import { buttonVariants } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import {
  agentSessionsSearch,
  costText,
  searchAcrossHref,
  tokensText,
  totalTokens,
  whoText,
  type SearchAcross,
  type AgentSessionFilters,
  type AgentSessionRow,
  type AgentSessionTotals,
  type RunFilter,
} from '@/lib/agentSessions'
import { rangeOfAnswer, type AnsweredRange } from '@/lib/performance'
import type { TimeRange } from '@/lib/timeRange'
import type { SharedProps } from '@/types'

type AgentSessionsProps =
  | { on: false; can_turn_on: boolean; settings_path: string }
  | {
      on: true
      settings_path: string
      range: AnsweredRange
      filters: AgentSessionFilters
      event_store: { available: boolean; error?: string }
      rows: AgentSessionRow[]
      totals: AgentSessionTotals
      more: boolean
      cost_label: string
      people: { id: number; name: string }[]
      projects: { name: string; slug: string }[]
      repositories: string[]
      search: SearchAcross
    }

const ALL = '__all'

const DESCRIPTION = 'Each Agent session of this Organization: the engineers\' in the captured repositories, and each Run\'s.'

/**
 * The Agent sessions page: the Agent sessions of the Organization's Agent
 * Project that the filters select, latest activity first, with the totals
 * of their tokens and estimated cost. The range and the filters are in the
 * URL, so that a filtered view can be shared.
 */
export default function AgentSessionsIndex({ agent_sessions: view }: { agent_sessions: AgentSessionsProps }) {
  return (
    <DashboardLayout>
      <PageHeader title="Agent sessions" description={DESCRIPTION} />
      {view.on ? <SessionsList view={view} /> : <TurnOn canTurnOn={view.can_turn_on} settingsPath={view.settings_path} />}
    </DashboardLayout>
  )
}

function TurnOn({ canTurnOn, settingsPath }: { canTurnOn: boolean; settingsPath: string }) {
  return (
    <Card className="border-dashed" data-notice="agent-sessions-off">
      <CardContent className="flex flex-col items-center justify-center py-12 text-center">
        <Bot className="mb-4 h-12 w-12 text-muted-foreground opacity-50" aria-hidden />
        <h2 className="text-lg font-medium">Agent sessions are off</h2>
        <p className="mb-4 max-w-md text-sm text-muted-foreground">
          {canTurnOn
            ? 'Turn them on in Settings → Agent sessions, with the repositories whose Agent sessions upload.'
            : 'An Admin of this Organization turns them on in Settings → Agent sessions.'}
        </p>
        <Link href={settingsPath} className={buttonVariants({ variant: canTurnOn ? 'default' : 'outline' })}>
          {canTurnOn ? 'Turn on Agent sessions' : 'Open the settings'}
        </Link>
      </CardContent>
    </Card>
  )
}

function SessionsList({ view }: { view: Extract<AgentSessionsProps, { on: true }> }) {
  const { current_org } = usePage<SharedProps>().props
  const range = rangeOfAnswer(view.range)
  const path = `/${current_org?.slug ?? ''}/agent_sessions`
  const visit = (nextRange: TimeRange, filters: AgentSessionFilters) => router.visit(`${path}${agentSessionsSearch(nextRange, filters)}`)
  const filter = (change: Partial<AgentSessionFilters>) => visit(range, { ...view.filters, ...change })

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <AgentSessionSearch
          label="Search across Agent sessions"
          placeholder="Search the text of all Agent sessions…"
          onSearch={text => router.visit(searchAcrossHref(view.search, text))}
        />
        <p className="text-xs text-muted-foreground">
          Opens the Logs explorer on the Agent Project, over the last 7 days. Choose a longer range there; it takes longer.
        </p>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex flex-wrap items-center border">
          <FilterSelect
            label="Person"
            all="All people"
            value={view.filters.person === null ? null : String(view.filters.person)}
            options={view.people.map(p => ({ value: String(p.id), label: p.name }))}
            onChange={value => filter({ person: value === null ? null : Number(value) })}
          />
          <FilterSelect
            label="Repository"
            all="All repositories"
            value={view.filters.repository}
            options={view.repositories.map(r => ({ value: r, label: r }))}
            onChange={value => filter({ repository: value })}
          />
          <FilterSelect
            label="Project (Runs)"
            all="All Projects (Runs)"
            value={view.filters.project}
            options={view.projects.map(p => ({ value: p.slug, label: p.name }))}
            onChange={value => filter({ project: value })}
          />
          <FilterSelect
            label="Run or no Run"
            all="Runs and no Run"
            value={view.filters.run}
            options={[{ value: 'run', label: 'Runs only' }, { value: 'none', label: 'No Run' }]}
            onChange={value => filter({ run: value as RunFilter | null })}
            last
          />
        </div>
        <TimeRangePicker range={range} onChange={next => visit(next, view.filters)} />
      </div>

      {!view.event_store.available && (
        <div role="alert" className="flex items-start gap-2 border border-warning/40 px-3 py-2 text-sm">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
          <span>{view.event_store.error}</span>
        </div>
      )}

      {view.event_store.available && <Totals totals={view.totals} costLabel={view.cost_label} />}

      {view.event_store.available && view.rows.length === 0 ? (
        <Card className="border-dashed" data-notice="no-agent-sessions">
          <CardContent className="flex flex-col items-center justify-center py-12 text-center">
            <Bot className="mb-4 h-12 w-12 text-muted-foreground opacity-50" aria-hidden />
            <h2 className="text-lg font-medium">No Agent sessions in this range</h2>
            <p className="max-w-md text-sm text-muted-foreground">
              Change the range or the filters. To upload Agent sessions, engineers set up capture from Settings → Agent sessions.
            </p>
          </CardContent>
        </Card>
      ) : (
        view.rows.length > 0 && <SessionsTable rows={view.rows} more={view.more} total={view.totals.agent_sessions} path={path} />
      )}
    </div>
  )
}

function FilterSelect({ label, all, value, options, onChange, last }: {
  label: string
  all: string
  value: string | null
  options: { value: string; label: string }[]
  onChange: (value: string | null) => void
  last?: boolean
}) {
  const listed = value !== null && !options.some(o => o.value === value) ? [...options, { value, label: value }] : options
  const items: Record<string, string> = Object.fromEntries([[ALL, all], ...listed.map(o => [o.value, o.label])])
  return (
    <Select value={value ?? ALL} items={items} onValueChange={next => next && onChange(next === ALL ? null : next)}>
      <SelectTrigger aria-label={label} className={last ? 'border-0' : 'border-0 border-r'}><SelectValue /></SelectTrigger>
      <SelectContent>
        {Object.entries(items).map(([v, text]) => <SelectItem key={v} value={v}>{text}</SelectItem>)}
      </SelectContent>
    </Select>
  )
}

function Totals({ totals, costLabel }: { totals: AgentSessionTotals; costLabel: string }) {
  const stats = [
    { label: 'Agent sessions', value: totals.agent_sessions.toLocaleString() },
    { label: 'Tokens', value: tokensText(totalTokens(totals)), title: `${totals.input_tokens.toLocaleString()} input, ${totals.output_tokens.toLocaleString()} output, ${totals.cache_read_tokens.toLocaleString()} cache read, ${totals.cache_write_tokens.toLocaleString()} cache write` },
    { label: 'Input', value: tokensText(totals.input_tokens) },
    { label: 'Output', value: tokensText(totals.output_tokens) },
    { label: 'Cache read', value: tokensText(totals.cache_read_tokens) },
    { label: 'Cache write', value: tokensText(totals.cache_write_tokens) },
  ]
  return (
    <section aria-label="Totals of the rows" className="grid grid-cols-2 border sm:grid-cols-4 lg:grid-cols-7">
      {stats.map(s => (
        <div key={s.label} className="border-b border-r px-3 py-2 last:border-r-0 lg:border-b-0" title={s.title}>
          <div className="text-xs text-muted-foreground">{s.label}</div>
          <div className="font-mono text-sm tabular-nums">{s.value}</div>
        </div>
      ))}
      <div className="col-span-2 px-3 py-2 sm:col-span-4 lg:col-span-1">
        <div className="text-xs text-muted-foreground">Cost, {costLabel}</div>
        <div className="font-mono text-sm tabular-nums">{costText(totals.cost_usd)}</div>
        {totals.no_cost > 0 && (
          <div className="text-xs text-muted-foreground">{totals.no_cost.toLocaleString()} with no cost not included</div>
        )}
      </div>
    </section>
  )
}

function SessionsTable({ rows, more, total, path }: { rows: AgentSessionRow[]; more: boolean; total: number; path: string }) {
  return (
    <div className="space-y-2">
      <div className="overflow-x-auto border">
        <Table aria-label="Agent sessions">
          <TableHeader>
            <TableRow>
              <TableHead>Start</TableHead>
              <TableHead className="max-md:hidden">Last activity</TableHead>
              <TableHead>Person or Run</TableHead>
              <TableHead className="max-md:hidden">Repository</TableHead>
              <TableHead className="max-md:hidden">Model</TableHead>
              <TableHead className="text-right">Tokens</TableHead>
              <TableHead className="text-right max-md:hidden">Estimated cost</TableHead>
              <TableHead className="text-right max-2xl:hidden">Messages</TableHead>
              <TableHead className="text-right max-2xl:hidden">Tool calls</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map(row => (
              <TableRow key={row.id} data-session={row.id} className="cursor-pointer" onClick={e => openRow(e, `${path}/${row.id}`)}>
                <TableCell className="font-mono text-xs whitespace-nowrap">{timeText(row.first)}</TableCell>
                <TableCell className="font-mono text-xs whitespace-nowrap max-md:hidden">{timeText(row.last)}</TableCell>
                <TableCell className="max-md:whitespace-normal max-md:break-all">
                  <Link href={`${path}/${row.id}`} className={`underline-offset-2 hover:underline ${row.run ? 'font-mono text-xs' : ''}`}>{whoText(row)}</Link>
                  {row.flags.map(flag => <Badge key={flag} variant="warn" className="ml-1.5">{flag}</Badge>)}
                  {/* Below md the Repository column is hidden, so that the table fits. */}
                  <div className="mt-1 font-mono text-xs break-all text-muted-foreground md:hidden">{row.repository}{row.folder && `/${row.folder}`}</div>
                </TableCell>
                <TableCell className="font-mono text-xs max-md:hidden">
                  {row.repository}
                  {row.folder && <span className="text-muted-foreground">/{row.folder}</span>}
                </TableCell>
                <TableCell className="font-mono text-xs max-md:hidden">{row.model || <span className="font-sans text-muted-foreground">None</span>}</TableCell>
                <TableCell
                  className="text-right font-mono text-xs tabular-nums"
                  title={`${row.input_tokens.toLocaleString()} input, ${row.output_tokens.toLocaleString()} output, ${row.cache_read_tokens.toLocaleString()} cache read, ${row.cache_write_tokens.toLocaleString()} cache write`}
                >
                  {tokensText(totalTokens(row))}
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums max-md:hidden">
                  {row.cost_usd === null ? <span className="font-sans text-muted-foreground">No cost</span> : costText(row.cost_usd)}
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums max-2xl:hidden">{row.person_messages.toLocaleString()}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums max-2xl:hidden">{row.tool_calls.toLocaleString()}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      {more && (
        <p className="text-xs text-muted-foreground">
          The newest {rows.length.toLocaleString()} of {total.toLocaleString()} Agent sessions. The totals are of all {total.toLocaleString()}. Narrow the range or the filters to see the others.
        </p>
      )}
    </div>
  )
}

/**
 * Opens an Agent session when a click lands anywhere on its row. A click on
 * the name's link is the link's own, and a click that ends a text selection
 * does not open, so that the row's text can still be copied. Ctrl or Cmd
 * opens a new tab, as on the link.
 */
function openRow(e: React.MouseEvent<HTMLTableRowElement>, href: string) {
  if ((e.target as HTMLElement).closest('a')) return
  if (window.getSelection()?.toString()) return
  if (e.metaKey || e.ctrlKey) {
    window.open(href, '_blank')
    return
  }
  router.visit(href)
}

function timeText(time: string): string {
  return format(new Date(time), 'MMM d, HH:mm')
}
