import { useState } from 'react'
import { Link, router, usePage } from '@inertiajs/react'
import { format, formatDistanceToNow } from 'date-fns'
import { Pencil, Play, Power, Trash2 } from 'lucide-react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import {
  AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { valueText } from '@/lib/charts/axis'
import { providerName } from '@/lib/integrations'
import { groupLabel, readingText, ruleText, runText, type Monitor } from '@/lib/monitors/monitor'
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
  const comparesChange = monitor.kind === 'threshold' && monitor.compare === 'change'

  const visit = (what: 'run' | 'power', method: 'post' | 'patch', url: string) => {
    setBusy(what)
    router[method](url, {}, { preserveScroll: true, onFinish: () => setBusy(null) })
  }

  return (
    <DashboardLayout>
      <PageHeader
        breadcrumbs={[{ label: 'Monitors', href: `/${orgSlug}/monitors` }, { label: monitor.name }]}
        title={monitor.name}
        badge={<MonitorStatusBadge monitor={monitor} />}
        description={<>
          {monitor.kind === 'threshold' ? 'Threshold' : 'Match'} monitor on{' '}
          <Link href={`/${orgSlug}/projects/${monitor.project.slug}/logs`} className="hover:underline">{monitor.project.name}</Link>
          {' · '}{monitor.last_run_at ? `last run ${formatDistanceToNow(new Date(monitor.last_run_at))} ago` : 'not run yet'}
        </>}
        actions={<>
          <Link href={`${base}/edit`} className={buttonVariants({ variant: 'outline' })}>
            <Pencil className="h-4 w-4" /> Edit
          </Link>
          {monitor.enabled && (
            <Button variant="outline" onClick={() => visit('run', 'post', `${base}/run`)} disabled={busy !== null}>
              <Play className="h-4 w-4" /> {busy === 'run' ? 'Running…' : 'Run now'}
            </Button>
          )}
          <Button
            variant="outline"
            onClick={() => visit('power', 'patch', `${base}/${monitor.enabled ? 'disable' : 'enable'}`)}
            disabled={busy !== null}
          >
            <Power className="h-4 w-4" /> {monitor.enabled ? 'Turn off' : 'Turn on'}
          </Button>
          <Button variant="outline" onClick={() => setDeleting(true)} className="text-destructive">
            <Trash2 className="h-4 w-4" /> Delete
          </Button>
        </>}
      />

      <div className="space-y-6">
        {monitor.last_error && (
          <div role="alert" className="border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
            The last run failed: {monitor.last_error}
          </div>
        )}

        <section className="space-y-2 border p-4 text-sm">
          <pre className="overflow-x-auto whitespace-pre-wrap break-words bg-muted p-2 font-mono text-xs" data-monitor-query>{monitor.query}</pre>
          <p>{ruleText(monitor)}</p>
          <p className="text-muted-foreground">
            {integrations.length > 0
              ? <>Tells {integrations.map(i => `${i.name} (${providerName(i.provider)})${i.enabled ? '' : ', off'}`).join(', ')}.</>
              : <>Tells no Integrations: its runs are recorded here only. <Link href={`${base}/edit`} className="underline">Add one</Link>.</>}
          </p>
        </section>

        {monitor.kind === 'threshold' && (
          <section className="space-y-2">
            <h2 className="text-sm font-semibold">Groups</h2>
            {states.length === 0 ? (
              <p className="text-sm text-muted-foreground">No groups yet: a run lists each group its Query finds, and keeps the firing ones and those just resolved.</p>
            ) : (
              <div className="border">
                <Table aria-label="Groups">
                  <TableHeader>
                    <TableRow>
                      <TableHead>Group</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Since</TableHead>
                      <TableHead className="text-right">{comparesChange ? 'Last value, Baseline and change' : 'Last value'}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {states.map(s => (
                      <TableRow key={s.group_key} data-group-status={s.status}>
                        <TableCell className="font-mono text-xs">{groupLabel(s.group)}</TableCell>
                        <TableCell>
                          <Badge variant={s.status === 'firing' ? 'error' : 'ok'} dot>{s.status === 'firing' ? 'Firing' : 'OK'}</Badge>
                        </TableCell>
                        <TableCell className="font-mono text-xs text-muted-foreground">{format(new Date(s.since), 'PPp')}</TableCell>
                        <TableCell className="text-right font-mono text-xs" title={s.last_value === null ? undefined : String(s.last_value)} data-group-reading>
                          {comparesChange ? readingText(s.last_value, s.last_baseline) : s.last_value === null ? 'No data' : valueText(s.last_value)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </section>
        )}

        <section className="space-y-2">
          <h2 className="text-sm font-semibold">Runs</h2>
          {runs.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              {monitor.enabled ? `No runs yet. It runs within a minute of being saved, then every ${minutes(monitor.interval_minutes)}; Run now runs it at once.` : 'No runs yet. Turn it on to run it.'}
            </p>
          ) : (
            <div className="border">
              <Table aria-label="Runs">
                <TableHeader>
                  <TableRow>
                    <TableHead>Time</TableHead>
                    <TableHead>Result</TableHead>
                    <TableHead className="text-right">Took</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {runs.map(r => (
                    <TableRow key={r.id} data-run-status={r.status}>
                      <TableCell className="font-mono text-xs text-muted-foreground">{format(new Date(r.ran_at), 'PPpp')}</TableCell>
                      <TableCell className={r.status === 'error' ? 'text-destructive' : undefined}>{runText(r, monitor.kind)}</TableCell>
                      <TableCell className="text-right font-mono text-xs text-muted-foreground">{r.elapsed_ms === null ? '' : `${r.elapsed_ms} ms`}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </section>
      </div>

      <AlertDialog open={deleting} onOpenChange={setDeleting}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete "{monitor.name}"?</AlertDialogTitle>
            <AlertDialogDescription>
              The Monitor stops running and its run history goes. Each firing group is sent to its Integrations as resolved, which closes its PagerDuty incident.
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
