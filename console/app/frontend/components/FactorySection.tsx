import { useRef } from 'react'
import { Link, useForm, usePage } from '@inertiajs/react'
import { TriangleAlert } from 'lucide-react'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { FactoryRoutes, FactorySchedules } from '@/components/FactoryRoutesAndSchedules'
import { factorySectionShown } from '@/lib/dispatcher'
import type { FactorySectionData, FactorySectionPaths, FactorySettingsValues } from '@/lib/dispatcher'

/**
 * The Factory section of a Project's settings: the repository, folder and
 * default base branch that the Project's Runs use, the tracker routes and
 * the schedules. The page shows it only
 * with a Dispatcher URL; it loads the settings from the Dispatcher after
 * the page shows. Admins edit; members read.
 *
 * It does not use <Deferred>: after a save, the redirect has no `factory`
 * until the page loads it again, and <Deferred> would then unmount the form
 * with the Dispatcher's error and the typed values. The section shows the
 * last settings in that time (factorySectionShown).
 */
export function FactorySection({ paths, canEdit }: { paths: FactorySectionPaths; canEdit: boolean }) {
  const { factory } = usePage<{ factory?: FactorySectionData }>().props
  const last = useRef<FactorySectionData | undefined>(undefined)
  const shown = factorySectionShown(factory, last.current)
  last.current = shown
  return (
    <Card data-section="factory">
      <CardHeader>
        <CardTitle>Factory</CardTitle>
        <CardDescription>
          The code that this Project's Runs work on. The Dispatcher keeps these settings.
        </CardDescription>
      </CardHeader>
      {shown ? <FactoryBody factory={shown} paths={paths} canEdit={canEdit} /> : <FactoryLoading />}
    </Card>
  )
}

function FactoryLoading() {
  return (
    <CardContent className="space-y-3 pb-6">
      <Skeleton className="h-9 w-full" />
      <Skeleton className="h-9 w-full" />
      <Skeleton className="h-9 w-full" />
    </CardContent>
  )
}

function FactoryBody({ factory, paths, canEdit }: {
  factory: FactorySectionData
  paths: FactorySectionPaths
  canEdit: boolean
}) {
  if (!('settings' in factory)) {
    return (
      <>
        <CardContent className="pb-6">
          <p className="flex items-start gap-1.5 text-sm font-medium text-destructive" role="alert" data-testid="factory-error">
            <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
            {factory.error}
          </p>
        </CardContent>
        <CardFooter>
          <RunsLink paths={paths} />
        </CardFooter>
      </>
    )
  }
  // A new key after a save shows what the Dispatcher kept.
  return (
    <>
      <FactoryForm key={JSON.stringify(factory.settings)} paths={paths} canEdit={canEdit} settings={factory.settings} />
      <CardContent className="space-y-6 border-t pt-6 pb-6">
        <FactoryRoutes
          routes={factory.routes}
          trackerConnections={factory.tracker_connections ?? []}
          path={paths.routes_path}
          canEdit={canEdit}
          hasSettings={factory.settings !== null}
        />
        <FactorySchedules schedules={factory.schedules} />
      </CardContent>
    </>
  )
}

function RunsLink({ paths }: { paths: FactorySectionPaths }) {
  return (
    <Link href={paths.runs_path} className={buttonVariants({ variant: 'outline' })} data-action="open-runs">
      Open this Project's Runs
    </Link>
  )
}

const FIELDS: { name: keyof FactorySettingsValues; label: string; placeholder: string; hint: string }[] = [
  { name: 'repository', label: 'Repository', placeholder: 'bitbucket.org/workspace/name', hint: 'host/workspace/name, or a clone URL.' },
  { name: 'folder', label: 'Folder', placeholder: 'The root of the repository', hint: "The Project's folder in the repository." },
  { name: 'base_branch', label: 'Default base branch', placeholder: "The repository's default branch", hint: 'The branch that a Run starts from when it names none.' },
]

function FactoryForm({ paths, canEdit, settings }: {
  paths: FactorySectionPaths
  canEdit: boolean
  settings: FactorySettingsValues | null
}) {
  const form = useForm<FactorySettingsValues & { factory?: string }>({
    repository: settings?.repository ?? '',
    folder: settings?.folder ?? '',
    base_branch: settings?.base_branch ?? '',
  })
  form.transform(({ repository, folder, base_branch }) => ({ factory: { repository, folder, base_branch } }))

  const submit: React.FormEventHandler = (e) => {
    e.preventDefault()
    form.patch(paths.path, { preserveScroll: true })
  }

  return (
    <form onSubmit={submit}>
      <CardContent className="space-y-4 pb-6">
        {!settings && (
          <p className="text-sm text-muted-foreground" data-testid="factory-none">
            This Project has no Factory settings yet.{canEdit && ' Give a repository to send Runs to it.'}
          </p>
        )}
        {FIELDS.map((field) => (
          <div key={field.name} className="space-y-2">
            <Label htmlFor={`factory-${field.name}`}>{field.label}</Label>
            <Input
              id={`factory-${field.name}`}
              className="font-mono text-xs"
              value={form.data[field.name]}
              onChange={(e) => form.setData(field.name, e.target.value)}
              placeholder={field.placeholder}
              readOnly={!canEdit}
            />
            <p className="text-xs text-muted-foreground">{field.hint}</p>
          </div>
        ))}
        {form.errors.factory && (
          <p className="text-sm font-medium text-destructive" role="alert">{form.errors.factory}</p>
        )}
        {!canEdit && (
          <p className="text-xs text-muted-foreground">Only organization admins can change this.</p>
        )}
      </CardContent>
      <CardFooter className="justify-between gap-2">
        <RunsLink paths={paths} />
        {canEdit && (
          <Button type="submit" disabled={form.processing}>
            {form.processing ? 'Saving…' : 'Save'}
          </Button>
        )}
      </CardFooter>
    </form>
  )
}
