import { Link, router, usePage } from '@inertiajs/react'
import { format } from 'date-fns'
import { AlertTriangle, UserRound, Workflow } from 'lucide-react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { TimeRangePicker } from '@/components/TimeRangePicker'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { rangeOfAnswer, type AnsweredRange } from '@/lib/performance'
import {
  actorText,
  runsSearch,
  stateText,
  stateTone,
  waitingOnPersonFilters,
  WAITING_ON_PERSON,
  type RunFilters,
  type RunRow,
  type RunState,
} from '@/lib/runs'
import type { TimeRange } from '@/lib/timeRange'
import type { SharedProps } from '@/types'

type RunsProps =
  | { on: false; settings_path: string }
  | {
      on: true
      settings_path: string
      range: AnsweredRange
      filters: RunFilters
      event_store: { available: boolean; error?: string }
      rows: RunRow[]
      more: boolean
      projects: { name: string; slug: string }[]
      states: RunState[]
    }

const ALL = '__all'

const DESCRIPTION = 'Each Run of the factory in this Organization, from its run events. Active Runs come first.'

/**
 * The Runs page: the last run event of each Run of the Organization's Agent
 * Project in the range, that the Project and state filters select, Active
 * Runs first. The range and the filters are in the URL, so that a filtered
 * view can be shared.
 */
export default function RunsIndex({ runs: view }: { runs: RunsProps }) {
  return (
    <DashboardLayout>
      <PageHeader title="Runs" description={DESCRIPTION} />
      {view.on ? <RunsList view={view} /> : <Off settingsPath={view.settings_path} />}
    </DashboardLayout>
  )
}

function Off({ settingsPath }: { settingsPath: string }) {
  return (
    <Card className="border-dashed" data-notice="runs-off">
      <CardContent className="flex flex-col items-center justify-center py-12 text-center">
        <Workflow className="mb-4 h-12 w-12 text-muted-foreground opacity-50" aria-hidden />
        <h2 className="text-lg font-medium">No Agent Project</h2>
        <p className="mb-4 max-w-md text-sm text-muted-foreground">
          The factory records each Run in the Agent Project. An Admin makes it when they turn on Agent sessions in Settings → Agent sessions.
        </p>
        <Link href={settingsPath} className={buttonVariants({ variant: 'outline' })}>Open the settings</Link>
      </CardContent>
    </Card>
  )
}

function RunsList({ view }: { view: Extract<RunsProps, { on: true }> }) {
  const { current_org } = usePage<SharedProps>().props
  const range = rangeOfAnswer(view.range)
  const path = `/${current_org?.slug ?? ''}/runs`
  const visit = (nextRange: TimeRange, filters: RunFilters) => router.visit(`${path}${runsSearch(nextRange, filters)}`)
  const filter = (change: Partial<RunFilters>) => visit(range, { ...view.filters, ...change })
  const projectNames = Object.fromEntries(view.projects.map(p => [p.slug, p.name]))
  const waiting = view.filters.state === WAITING_ON_PERSON

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex flex-wrap items-center border">
          <FilterSelect
            label="Project"
            all="All Projects"
            value={view.filters.project}
            options={view.projects.map(p => ({ value: p.slug, label: p.name }))}
            onChange={value => filter({ project: value })}
          />
          <FilterSelect
            label="State"
            all="All states"
            value={view.filters.state}
            options={view.states.map(s => ({ value: s, label: stateText(s) }))}
            onChange={value => filter({ state: value as RunState | null })}
            last
          />
        </div>
        <Button
          variant={waiting ? 'default' : 'outline'}
          aria-pressed={waiting}
          onClick={() => visit(range, waitingOnPersonFilters(view.filters))}
        >
          <UserRound aria-hidden />
          Waiting on a person
        </Button>
        <TimeRangePicker range={range} onChange={next => visit(next, view.filters)} />
      </div>

      {!view.event_store.available && (
        <div role="alert" className="flex items-start gap-2 border border-warning/40 px-3 py-2 text-sm">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
          <span>{view.event_store.error}</span>
        </div>
      )}

      {view.event_store.available && view.rows.length === 0 ? (
        <Card className="border-dashed" data-notice="no-runs">
          <CardContent className="flex flex-col items-center justify-center py-12 text-center">
            <Workflow className="mb-4 h-12 w-12 text-muted-foreground opacity-50" aria-hidden />
            <h2 className="text-lg font-medium">No Runs in this range</h2>
            <p className="max-w-md text-sm text-muted-foreground">
              Change the range or the filters. A Run starts from a trigger in a Project's Factory settings, or from the CLI.
            </p>
          </CardContent>
        </Card>
      ) : (
        view.rows.length > 0 && <RunsTable rows={view.rows} more={view.more} projectNames={projectNames} />
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

function RunsTable({ rows, more, projectNames }: { rows: RunRow[]; more: boolean; projectNames: Record<string, string> }) {
  return (
    <div className="space-y-2">
      <div className="overflow-x-auto border">
        <Table aria-label="Runs">
          <TableHeader>
            <TableRow>
              <TableHead>State</TableHead>
              <TableHead>Run</TableHead>
              <TableHead>Project</TableHead>
              <TableHead>Repository</TableHead>
              <TableHead>Trigger</TableHead>
              <TableHead>Started by</TableHead>
              <TableHead>Last event</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map(row => (
              <TableRow key={row.id} data-run={row.id} data-active={row.active || undefined}>
                <TableCell className="align-top">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <Badge variant={stateTone(row.state)}>{stateText(row.state)}</Badge>
                    {row.active && <Badge variant="count">Active</Badge>}
                    {row.closed && <Badge>Closed</Badge>}
                  </div>
                  {row.reason && <div className="mt-1 max-w-56 text-xs text-muted-foreground">{row.reason}</div>}
                </TableCell>
                <TableCell className="align-top">
                  <div className="max-w-80 text-sm">{row.title || <span className="text-muted-foreground">No title</span>}</div>
                  <div className="font-mono text-xs text-muted-foreground">{row.id}</div>
                </TableCell>
                <TableCell className="align-top text-sm">{row.project ? (projectNames[row.project] ?? row.project) : <span className="text-muted-foreground">None</span>}</TableCell>
                <TableCell className="align-top font-mono text-xs">
                  {row.repository}
                  {row.folder && <span className="text-muted-foreground">/{row.folder}</span>}
                </TableCell>
                <TableCell className="align-top text-sm">{row.trigger}</TableCell>
                <TableCell className="align-top text-sm">{actorText(row.actor)}</TableCell>
                <TableCell className="align-top font-mono text-xs whitespace-nowrap">{format(new Date(row.time), 'MMM d, HH:mm')}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      {more && (
        <p className="text-xs text-muted-foreground">
          The first {rows.length.toLocaleString()} Runs, Active Runs first. Narrow the range or the filters to see the others.
        </p>
      )}
    </div>
  )
}
