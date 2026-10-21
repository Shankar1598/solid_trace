import { Link, router, usePage } from '@inertiajs/react'
import { AlertTriangle } from 'lucide-react'
import { useEffect, useState } from 'react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { QueryEditor } from '@/components/QueryEditor'
import {
  AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { checkProgram } from '@/lib/logTransform/api'
import { failuresHref, marksOf, type ProgramCheck, type ProgramMark } from '@/lib/logTransform/program'
import type { Project, SharedProps } from '@/types'

// How long after the last keystroke the draft is checked, as the Monitor
// form waits before checking its Query.
const CHECK_DELAY_MS = 500

interface Version {
  id: number
  program: string
  /** Who saved it; null once they're gone. */
  saved_by: string | null
  saved_at: string
}

/** The current version's running counts since EventStore started or the version was saved. */
interface Stats {
  version_id: number | null
  processed: number
  dropped: number
  failed: number
  average_micros: number
  /** Why the saved program doesn't compile, as after an upgrade changed VRL. */
  compile_error?: string
}

interface LogTransformShowProps {
  project: Project
  /** The current program; empty when there's none. */
  program: string
  /** The newest versions first, up to 20. */
  versions: Version[]
  /** Null when EventStore couldn't give them. */
  stats: Stats | null
  can_manage: boolean
  /** VRL's errors when a save was refused. */
  errors?: { program?: string[] }
}

/** What the check said about the draft it was given, with its underlines placed in that text. */
type Checked = { program: string; check: ProgramCheck; marks: ProgramMark[] } | { program: string; failed: string }

/**
 * A Project's Log transform: the program in an editor that underlines its
 * errors as it's typed, its running counts and its last versions. Admins
 * save and restore; members read it.
 */
export default function LogTransformShow({ project, program, versions, stats, can_manage, errors }: LogTransformShowProps) {
  const { current_org } = usePage<SharedProps>().props
  const orgSlug = current_org?.slug ?? ''
  const base = `/${orgSlug}/projects/${project.slug}/log_transform`
  const [draft, setDraft] = useState(program)
  // A save or restore makes a new current version, whose program replaces the draft: Inertia keeps
  // the page's state across them. A refused save makes none, so the draft stays to be fixed.
  const currentVersion = versions[0]?.id ?? null
  const [draftOf, setDraftOf] = useState(currentVersion)
  if (draftOf !== currentVersion) {
    setDraftOf(currentVersion)
    setDraft(program)
  }
  const [checked, setChecked] = useState<Checked | null>(null)
  const [confirming, setConfirming] = useState<{ kind: 'save' } | { kind: 'restore'; version: Version } | null>(null)
  const [viewing, setViewing] = useState<Version | null>(null)
  const [saving, setSaving] = useState(false)

  // The draft is checked a moment after the last change; a newer draft drops the older check.
  useEffect(() => {
    const controller = new AbortController()
    const timer = setTimeout(() => {
      checkProgram({ orgSlug, projectSlug: project.slug, program: draft, signal: controller.signal })
        .then(check => setChecked({ program: draft, check, marks: marksOf(draft, check) }))
        .catch(error => {
          if (!controller.signal.aborted) setChecked({ program: draft, failed: error instanceof Error ? error.message : String(error) })
        })
    }, CHECK_DELAY_MS)
    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [draft, orgSlug, project.slug])

  // The last check's underlines and problems stay while the next is typed, the editor moving them along with edits.
  const last = checked && 'check' in checked ? checked : null
  const problems = last ? [...last.check.errors, ...last.check.warnings] : []
  const hasErrors = last?.program === draft && last.check.errors.length > 0

  if (!current_org) return null

  const save = () => {
    setConfirming(null)
    setSaving(true)
    router.patch(base, { program: draft }, { preserveScroll: true, onFinish: () => setSaving(false) })
  }

  const restore = (version: Version) => {
    setConfirming(null)
    setViewing(null)
    setSaving(true)
    router.post(`${base}/restore`, { version_id: version.id }, { preserveScroll: true, onFinish: () => setSaving(false) })
  }

  const breadcrumbs = [
    { label: 'Projects', href: `/${current_org.slug}/projects` },
    { label: project.name, href: `/${current_org.slug}/projects/${project.slug}` },
    { label: 'Log transform' },
  ]
  const unchanged = draft === program

  return (
    <DashboardLayout>
      <PageHeader
        breadcrumbs={breadcrumbs}
        title="Log transform"
        description="A VRL program that Log processing runs on each of this Project's Logs before storing it."
      />
      <div className="max-w-4xl space-y-6">
        {stats?.compile_error && (
          <div role="alert" className="flex gap-3 border border-destructive bg-destructive/10 p-4 text-sm">
            <AlertTriangle className="h-5 w-5 shrink-0 text-destructive" />
            <div className="min-w-0 space-y-2">
              <p className="font-medium">The saved program doesn't compile, so Logs are being stored as they arrived until it's fixed.</p>
              <pre className="overflow-x-auto font-mono text-xs whitespace-pre-wrap">{stats.compile_error}</pre>
            </div>
          </div>
        )}

        <Card>
          <CardHeader>
            <CardTitle>Program</CardTitle>
            <CardDescription>
              It can set, change and remove Fields, and drop a Log with abort. A Log it fails on is stored as it arrived, with the error in
              {' '}<code className="font-mono text-xs">solidtrace.transform_error</code>. An empty program is none.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <QueryEditor
              value={draft}
              onChange={setDraft}
              language="vrl"
              label="Program"
              placeholder={can_manage ? '.status = "ok"' : 'No program: Logs are stored as they arrive.'}
              marks={last?.marks}
              minLines={12}
              maxLines={30}
              readOnly={!can_manage}
              describedBy={problems.length > 0 ? 'program-problems' : undefined}
            />
            <CheckLine checked={checked} draft={draft} />
            {problems.length > 0 && (
              <div id="program-problems" className="space-y-2">
                {problems.map((p, i) => (
                  <pre key={i} className="overflow-x-auto bg-muted p-2 font-mono text-xs whitespace-pre-wrap">{p.text || p.message}</pre>
                ))}
              </div>
            )}
            {errors?.program && errors.program.length > 0 && (
              <div className="space-y-2">
                <p className="text-sm font-medium text-destructive">Not saved: the program doesn't compile.</p>
                {errors.program.map((text, i) => (
                  <pre key={i} className="overflow-x-auto bg-muted p-2 font-mono text-xs whitespace-pre-wrap">{text}</pre>
                ))}
              </div>
            )}
          </CardContent>
          <CardFooter className="justify-between gap-4">
            {can_manage ? (
              <>
                <p className="text-xs text-muted-foreground">A save applies to the Logs processed after it. Logs already stored don't change.</p>
                <Button onClick={() => setConfirming({ kind: 'save' })} disabled={saving || unchanged || hasErrors}>
                  {saving ? 'Saving…' : 'Save'}
                </Button>
              </>
            ) : (
              <p className="text-xs text-muted-foreground">Only Organization admins can save or restore the Log transform.</p>
            )}
          </CardFooter>
        </Card>

        {/* The preview of a draft on recent arrivals goes here (ticket 07). */}

        <RunningCounts stats={stats} failedHref={failuresHref(current_org.slug, project.slug)} />

        <Card>
          <CardHeader>
            <CardTitle>Versions</CardTitle>
            <CardDescription>Every save is kept. Restoring a version saves it again as the newest.</CardDescription>
          </CardHeader>
          <CardContent>
            {versions.length === 0 ? (
              <p className="text-sm text-muted-foreground">No versions yet.</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Saved</TableHead>
                    <TableHead>By</TableHead>
                    <TableHead className="text-right"><span className="sr-only">Actions</span></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {versions.map((version, i) => (
                    <TableRow key={version.id}>
                      <TableCell>
                        <span title={version.saved_at}>{new Date(version.saved_at).toLocaleString()}</span>
                        {i === 0 && <Badge variant="neutral" className="ml-2">Current</Badge>}
                        {version.program === '' && <span className="ml-2 text-xs text-muted-foreground">(empty)</span>}
                      </TableCell>
                      <TableCell>{version.saved_by ?? <span className="text-muted-foreground">A former user</span>}</TableCell>
                      <TableCell className="space-x-2 text-right">
                        <Button variant="outline" size="sm" onClick={() => setViewing(version)} aria-label={`View the version saved ${version.saved_at}`}>
                          View
                        </Button>
                        {can_manage && i > 0 && (
                          <Button
                            variant="outline"
                            size="sm"
                            disabled={saving}
                            onClick={() => setConfirming({ kind: 'restore', version })}
                            aria-label={`Restore the version saved ${version.saved_at}`}
                          >
                            Restore
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>

      <Dialog open={viewing !== null} onOpenChange={open => { if (!open) setViewing(null) }}>
        <DialogContent className="sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>
              {viewing && `Saved ${new Date(viewing.saved_at).toLocaleString()}${viewing.saved_by ? ` by ${viewing.saved_by}` : ''}`}
            </DialogTitle>
          </DialogHeader>
          <pre className="max-h-[60vh] overflow-auto bg-muted p-3 font-mono text-xs whitespace-pre-wrap">
            {viewing?.program || 'An empty program: none.'}
          </pre>
        </DialogContent>
      </Dialog>

      <AlertDialog open={confirming !== null} onOpenChange={open => { if (!open) setConfirming(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirming?.kind === 'restore' ? 'Restore this version?' : 'Save the Log transform?'}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirming?.kind === 'restore' ? 'It is saved again as the newest version. ' : ''}
              Log processing runs it on the Logs processed from now on, within seconds. Logs already stored don't change.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            {confirming?.kind === 'restore' ? (
              <Button onClick={() => restore(confirming.version)}>Restore</Button>
            ) : (
              <Button onClick={save}>Save</Button>
            )}
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </DashboardLayout>
  )
}

/** Whether the draft is being checked, compiles, or can't be checked. */
function CheckLine({ checked, draft }: { checked: Checked | null; draft: string }) {
  if (!checked || checked.program !== draft) return <p className="text-xs text-muted-foreground">Checking…</p>
  if ('failed' in checked) return <p className="text-xs text-destructive">{checked.failed}</p>
  const { errors, warnings } = checked.check
  if (errors.length === 0 && warnings.length === 0) return <p className="text-xs text-muted-foreground">It compiles.</p>
  const count = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`
  return (
    <p className={errors.length > 0 ? 'text-xs text-destructive' : 'text-xs text-muted-foreground'}>
      {[errors.length > 0 && count(errors.length, 'error'), warnings.length > 0 && count(warnings.length, 'warning')].filter(Boolean).join(', ')}
    </p>
  )
}

/** The current version's counts, the failed count linking to the Logs it failed on. */
function RunningCounts({ stats, failedHref }: { stats: Stats | null; failedHref: string }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Running counts</CardTitle>
        <CardDescription>The current version's, since EventStore started or the version was saved.</CardDescription>
      </CardHeader>
      <CardContent>
        {stats === null ? (
          <p className="text-sm text-muted-foreground">EventStore couldn't give the running counts. Reload the page to try again.</p>
        ) : stats.version_id === null ? (
          <p className="text-sm text-muted-foreground">No Log transform: Logs are stored as they arrive.</p>
        ) : (
          <dl className="grid grid-cols-[12rem_1fr] gap-y-2 text-sm">
            <dt className="text-muted-foreground">Processed</dt>
            <dd className="font-mono text-xs leading-5">{stats.processed.toLocaleString()}</dd>
            <dt className="text-muted-foreground">Dropped</dt>
            <dd className="font-mono text-xs leading-5">{stats.dropped.toLocaleString()}</dd>
            <dt className="text-muted-foreground">Failed</dt>
            <dd className="font-mono text-xs leading-5">
              <Link href={failedHref} className="underline underline-offset-2">{stats.failed.toLocaleString()}</Link>
            </dd>
            <dt className="text-muted-foreground">Average time per Log</dt>
            <dd className="font-mono text-xs leading-5">{stats.average_micros.toLocaleString()} µs</dd>
          </dl>
        )}
      </CardContent>
    </Card>
  )
}
