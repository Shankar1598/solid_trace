import { useState } from 'react'
import { Link, router, usePage } from '@inertiajs/react'
import { format, formatDistanceToNow } from 'date-fns'
import { Pencil, Play, Power, Trash2 } from 'lucide-react'
import Breadcrumbs from '@/components/Breadcrumbs'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import {
  AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button, buttonVariants } from '@/components/ui/button'
import { groupLabel, runText, type Monitor } from '@/lib/monitors/monitor'
import type { SharedProps } from '@/types'
import { MonitorStatusBadge } from './components/MonitorStatusBadge'

/**
 * One Monitor: what it runs and whom it tells, each group's status, and its
 * run history, with Edit, Run now, turning it off or on, and Delete.
 */
export default function MonitorsShow({ monitor }: { monitor: Monitor }) {
  const orgSlug = usePage<SharedProps>().props.current_org?.slug ?? ''
  const base = `/${orgSlug}/monitors/${monitor.id}`
  const [busy, setBusy] = useState<'run' | 'power' | null>(null)
  const [deleting, setDeleting] = useState(false)
  const states = monitor.states ?? []
  const runs = monitor.runs ?? []
  const integrations = monitor.integrations ?? []

  const visit = (what: 'run' | 'power', method: 'post' | 'patch', url: string) => {
    setBusy(what)
    router[method](url, {}, { preserveScroll: true, onFinish: () => setBusy(null) })
  }

  return (
    <DashboardLayout breadcrumbs={<Breadcrumbs items={[{ label: 'Monitors', href: `/${orgSlug}/monitors` }, { label: monitor.name }]} />}>
      <div className="space-y-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 space-y-1">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-2xl font-bold tracking-tight break-words">{monitor.name}</h1>
              <MonitorStatusBadge monitor={monitor} />
            </div>
            <p className="text-sm text-muted-foreground">
              {monitor.kind === 'threshold' ? 'Threshold' : 'Match'} monitor on{' '}
              <Link href={`/${orgSlug}/projects/${monitor.project.slug}/logs`} className="hover:underline">{monitor.project.name}</Link>
              {' · '}{monitor.last_run_at ? `last run ${formatDistanceToNow(new Date(monitor.last_run_at))} ago` : 'not run yet'}
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Link href={`${base}/edit`} className={buttonVariants({ variant: 'outline' })}>
              <Pencil className="mr-2 h-4 w-4" /> Edit
            </Link>
            {monitor.enabled && (
              <Button variant="outline" onClick={() => visit('run', 'post', `${base}/run`)} disabled={busy !== null}>
                <Play className="mr-2 h-4 w-4" /> {busy === 'run' ? 'Running…' : 'Run now'}
              </Button>
            )}
            <Button
              variant="outline"
              onClick={() => visit('power', 'patch', `${base}/${monitor.enabled ? 'disable' : 'enable'}`)}
              disabled={busy !== null}
            >
              <Power className="mr-2 h-4 w-4" /> {monitor.enabled ? 'Turn off' : 'Turn on'}
            </Button>
            <Button variant="outline" onClick={() => setDeleting(true)} className="text-destructive">
              <Trash2 className="mr-2 h-4 w-4" /> Delete
            </Button>
          </div>
        </div>

        {monitor.last_error && (
          <div role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
            The last run failed: {monitor.last_error}
          </div>
        )}

        <section className="space-y-2 rounded-md border p-4 text-sm">
          <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded bg-muted p-2 font-mono text-xs" data-monitor-query>{monitor.query}</pre>
          <p>{ruleText(monitor)}</p>
          <p className="text-muted-foreground">
            {integrations.length > 0
              ? <>Tells {integrations.map(i => `${i.name} (${i.provider})${i.enabled ? '' : ', off'}`).join(', ')}.</>
              : <>Tells no Integrations: its runs are recorded here only. <Link href={`${base}/edit`} className="underline">Add one</Link>.</>}
          </p>
        </section>

        {monitor.kind === 'threshold' && (
          <section className="space-y-2">
            <h2 className="text-lg font-semibold">Groups</h2>
            {states.length === 0 ? (
              <p className="text-sm text-muted-foreground">No groups yet: a run lists each group its Query finds, and keeps the firing ones and those just resolved.</p>
            ) : (
              <div className="overflow-x-auto rounded-md border">
                <table className="w-full text-sm" aria-label="Groups">
                  <thead className="bg-muted text-left text-xs text-muted-foreground">
                    <tr>
                      <th className="px-4 py-2 font-medium">Group</th>
                      <th className="px-4 py-2 font-medium">Status</th>
                      <th className="px-4 py-2 font-medium">Since</th>
                      <th className="px-4 py-2 font-medium">Last value</th>
                    </tr>
                  </thead>
                  <tbody>
                    {states.map(s => (
                      <tr key={s.group_key} className="border-t" data-group-status={s.status}>
                        <td className="px-4 py-2 font-mono text-xs">{groupLabel(s.group)}</td>
                        <td className={s.status === 'firing' ? 'px-4 py-2 font-medium text-destructive' : 'px-4 py-2'}>{s.status === 'firing' ? 'Firing' : 'OK'}</td>
                        <td className="px-4 py-2 text-muted-foreground">{format(new Date(s.since), 'PPp')}</td>
                        <td className="px-4 py-2 tabular-nums">{s.last_value ?? 'No data'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        )}

        <section className="space-y-2">
          <h2 className="text-lg font-semibold">Runs</h2>
          {runs.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              {monitor.enabled ? `No runs yet. It runs within a minute of being saved, then every ${minutes(monitor.interval_minutes)}; Run now runs it at once.` : 'No runs yet. Turn it on to run it.'}
            </p>
          ) : (
            <div className="overflow-x-auto rounded-md border">
              <table className="w-full text-sm" aria-label="Runs">
                <thead className="bg-muted text-left text-xs text-muted-foreground">
                  <tr>
                    <th className="px-4 py-2 font-medium">Time</th>
                    <th className="px-4 py-2 font-medium">Result</th>
                    <th className="px-4 py-2 font-medium">Took</th>
                  </tr>
                </thead>
                <tbody>
                  {runs.map(r => (
                    <tr key={r.id} className="border-t" data-run-status={r.status}>
                      <td className="whitespace-nowrap px-4 py-2 text-muted-foreground">{format(new Date(r.ran_at), 'PPpp')}</td>
                      <td className={r.status === 'error' ? 'px-4 py-2 text-destructive' : 'px-4 py-2'}>{runText(r, monitor.kind)}</td>
                      <td className="px-4 py-2 tabular-nums text-muted-foreground">{r.elapsed_ms === null ? '' : `${r.elapsed_ms} ms`}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>

      <AlertDialog open={deleting} onOpenChange={setDeleting}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete "{monitor.name}"?</AlertDialogTitle>
            <AlertDialogDescription>
              The Monitor stops running and its run history goes. Its Integrations are not told; a firing group stays open in PagerDuty.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button variant="destructive" onClick={() => router.delete(base)}>Delete monitor</Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </DashboardLayout>
  )
}

function minutes(n: number): string {
  return n === 1 ? 'minute' : `${n} minutes`
}

/** What the Monitor checks, in a sentence. */
function ruleText(monitor: Monitor): string {
  const every = `Runs every ${minutes(monitor.interval_minutes)}`
  if (monitor.kind === 'match') return `${every} and sends one message when Logs ingested since its last run match.`
  const groups = monitor.notify_by_group ? 'each group' : 'the worst group'
  const noData = monitor.alert_on_no_data ? ' No rows at all fire too.' : ''
  return `${every} over the last ${minutes(monitor.range_minutes)}, and fires when ${groups}'s number is ${monitor.operator} ${monitor.threshold}.${noData}`
}
