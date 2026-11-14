import { Link, useForm, usePage } from '@inertiajs/react'
import { FormEventHandler } from 'react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { FormSection } from '@/components/FormSection'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { addedEntries, parseEntries } from '@/lib/capturedRepositories'
import { SharedProps } from '@/types'

// One Admin's confirm of the capture notice: who, when, and the entries it was for.
interface Confirmation {
  user: string | null
  at: string
  entries: string[]
}

// Organization settings → Agent sessions (AgentSessionSettingsController).
interface AgentSessionsProps {
  agent_sessions: {
    on: boolean
    project: { name: string; slug: string; log_retention_days: number | null } | null
    captured_repositories: string[]
    confirmations: Confirmation[]
  }
  notice_text: string
  can_manage: boolean
}

export default function SettingsAgentSessions({ agent_sessions, notice_text, can_manage }: AgentSessionsProps) {
  const { current_org } = usePage<SharedProps>().props
  const path = `/${current_org!.slug}/settings/agent_sessions`
  const saved = agent_sessions.captured_repositories

  const { data, setData, post, patch, processing, errors, transform } = useForm({
    entries: saved.join('\n'),
    confirmed: false,
  })
  const draft = parseEntries(data.entries)
  const added = agent_sessions.on ? addedEntries(saved, draft) : draft
  const needsConfirm = added.length > 0

  // A form can't send an empty array, so [""] is the empty list.
  transform((data) => ({
    captured_repositories: draft.length > 0 ? draft : [''],
    confirmed: data.confirmed ? '1' : '0',
  }))

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    const options = { preserveScroll: true, onSuccess: () => setData('confirmed', false) }
    if (agent_sessions.on) patch(path, options)
    else post(path, options)
  }

  const formErrors = errors as Record<string, string | undefined>

  return (
    <SettingsLayout>
      <PageHeader
        title="Agent sessions"
        description="Record the Agent sessions of this Organization's engineers in its captured repositories. Each member reads them."
      />

      <FormSection
        title="Agent Project"
        description="The Project that holds the Organization's Agent sessions, one Log for each transcript line."
      >
        {agent_sessions.project ? (
          <p className="text-sm">
            On.{' '}
            <Link href={`/${current_org!.slug}/projects/${agent_sessions.project.slug}`} className="font-medium underline underline-offset-4">
              {agent_sessions.project.name}
            </Link>{' '}
            keeps its Logs for{' '}
            <span className="font-mono">{agent_sessions.project.log_retention_days ?? 'the default number of'}</span> days.
            Change its Retention in its settings. Engineers turn on capture with the{' '}
            <Link href={`${path}/setup`} className="font-medium underline underline-offset-4">Capture setup</Link>.
          </p>
        ) : (
          <p className="text-sm text-muted-foreground">
            Off. {can_manage ? 'Add at least one captured repository and confirm to turn it on.' : 'An Admin turns it on.'}
          </p>
        )}
      </FormSection>

      <FormSection
        className="mt-6"
        title="Captured repositories"
        description="Only Agent sessions in these repositories upload. Each entry is host/workspace/name, or host/workspace/* for each repository of a workspace."
      >
        {can_manage ? (
          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="captured_repositories">One entry on each line</Label>
              <Textarea
                id="captured_repositories"
                value={data.entries}
                onChange={(e) => setData('entries', e.target.value)}
                placeholder={'github.com/acme/shop\ngithub.com/acme/*'}
                className="min-h-24 font-mono"
                aria-invalid={!!formErrors.captured_repositories}
              />
              {formErrors.captured_repositories && (
                <p className="text-sm font-medium text-destructive">Captured repositories {formErrors.captured_repositories}</p>
              )}
              {formErrors.base && <p className="text-sm font-medium text-destructive">{formErrors.base}</p>}
            </div>

            {needsConfirm && (
              <div className="space-y-2">
                {agent_sessions.on && (
                  <p className="text-xs text-muted-foreground">
                    New capture: <span className="font-mono">{added.join(', ')}</span>
                  </p>
                )}
                <div className="flex items-start gap-2">
                  <Checkbox
                    id="confirmed"
                    checked={data.confirmed}
                    onCheckedChange={(value) => setData('confirmed', !!value)}
                    aria-invalid={!!formErrors.confirmed}
                  />
                  <Label htmlFor="confirmed" className="text-sm font-normal leading-snug">{notice_text}</Label>
                </div>
                {formErrors.confirmed && (
                  <p className="text-sm font-medium text-destructive">The confirm {formErrors.confirmed}</p>
                )}
              </div>
            )}

            <div className="flex justify-end">
              <Button type="submit" disabled={processing || (needsConfirm && !data.confirmed)}>
                {agent_sessions.on ? (processing ? 'Saving…' : 'Save') : (processing ? 'Turning on…' : 'Turn on Agent sessions')}
              </Button>
            </div>
          </form>
        ) : saved.length > 0 ? (
          <ul className="space-y-1 font-mono text-sm">
            {saved.map(entry => <li key={entry}>{entry}</li>)}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">None. No Agent session uploads.</p>
        )}
        {!can_manage && <p className="text-xs text-muted-foreground">Only an Admin can change this list.</p>}
      </FormSection>

      {agent_sessions.confirmations.length > 0 && (
        <FormSection className="mt-6" title="Confirms" description="Who confirmed the notice, when, and for which repositories.">
          <div className="border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>When</TableHead>
                  <TableHead>Who</TableHead>
                  <TableHead>Repositories</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {agent_sessions.confirmations.map(confirmation => (
                  <TableRow key={`${confirmation.at}-${confirmation.entries.join(',')}`}>
                    <TableCell className="font-mono text-xs" title={confirmation.at}>{new Date(confirmation.at).toLocaleString()}</TableCell>
                    <TableCell>{confirmation.user ?? 'A removed user'}</TableCell>
                    <TableCell className="font-mono text-xs whitespace-normal">{confirmation.entries.join(', ')}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </FormSection>
      )}
    </SettingsLayout>
  )
}
