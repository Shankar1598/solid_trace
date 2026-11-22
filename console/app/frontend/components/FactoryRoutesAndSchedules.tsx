import { useState } from 'react'
import { Link, router, useForm, usePage } from '@inertiajs/react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { routeMatchText, scheduleNextText } from '@/lib/dispatcher'
import type { FactoryRoute, FactorySchedule } from '@/lib/dispatcher'
import { stateText, stateTone } from '@/lib/runs'

/**
 * The tracker routes of the Factory section: which ready tickets go to
 * this Project. An Admin adds and removes routes; a member reads them. The
 * Dispatcher keeps the routes in the Factory settings, so a Project with
 * no settings has no routes.
 */
export function FactoryRoutes({ routes, trackerConnections, path, canEdit, hasSettings }: {
  routes: FactoryRoute[]
  trackerConnections: string[]
  path: string
  canEdit: boolean
  hasSettings: boolean
}) {
  const { errors } = usePage<{ errors: Record<string, string> }>().props
  const [removing, setRemoving] = useState<string | null>(null)
  const form = useForm({ connection: trackerConnections.length === 1 ? trackerConnections[0] : '', match: '' })
  form.transform(({ connection, match }) => ({ route: { connection, match } }))

  const add: React.FormEventHandler = (e) => {
    e.preventDefault()
    form.post(path, { preserveScroll: true, onSuccess: () => form.reset('match') })
  }
  const remove = (route: FactoryRoute) => {
    setRemoving(routeKey(route))
    router.delete(path, { data: { route: { connection: route.connection, match: route.match } }, preserveScroll: true, onFinish: () => setRemoving(null) })
  }

  return (
    <div className="space-y-3" data-section="factory-routes">
      <div>
        <h3 className="text-sm font-medium">Tracker routes</h3>
        <p className="text-xs text-muted-foreground">
          The ready tickets of a tracker connection that start Runs on this Project, by the value of the ticket's
          routing field.
        </p>
      </div>
      {routes.length === 0 ? (
        <p className="text-sm text-muted-foreground" data-testid="factory-no-routes">No tracker routes.</p>
      ) : (
        <ul className="divide-y border">
          {routes.map((route) => (
            <li key={routeKey(route)} className="flex items-center justify-between gap-3 px-3 py-2 text-sm" data-route={routeKey(route)}>
              <span className="min-w-0">
                <span className="font-mono text-xs">{route.connection}</span>
                <span className="text-muted-foreground"> → </span>
                <span>{routeMatchText(route.match)}</span>
              </span>
              {canEdit && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={removing !== null}
                  onClick={() => remove(route)}
                  data-action="remove-route"
                >
                  {removing === routeKey(route) ? 'Removing…' : 'Remove'}
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {errors.factory_routes && (
        <p className="text-sm font-medium text-destructive" role="alert" data-testid="factory-routes-error">
          {errors.factory_routes}
        </p>
      )}
      {canEdit && hasSettings && (
        <form onSubmit={add} className="grid gap-3 sm:grid-cols-[1fr_1fr_auto] sm:items-end" data-form="add-route">
          <div className="space-y-2">
            <Label htmlFor="factory-route-connection">Tracker connection</Label>
            <Input
              id="factory-route-connection"
              className="font-mono text-xs"
              list="factory-tracker-connections"
              value={form.data.connection}
              onChange={(e) => form.setData('connection', e.target.value)}
              placeholder="notion-main"
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="factory-route-match">Match value</Label>
            <Input
              id="factory-route-match"
              value={form.data.match}
              onChange={(e) => form.setData('match', e.target.value)}
              placeholder="Empty: each other ticket"
            />
          </div>
          <Button type="submit" variant="outline" disabled={form.processing} data-action="add-route">
            {form.processing ? 'Adding…' : 'Add route'}
          </Button>
          {/* Not in the field's box: space-y adds a bottom margin to the input above it, and the input then sits higher than its neighbour. */}
          <datalist id="factory-tracker-connections">
            {trackerConnections.map((name) => <option key={name} value={name} />)}
          </datalist>
        </form>
      )}
      {canEdit && !hasSettings && (
        <p className="text-xs text-muted-foreground">Save the Factory settings before you add a route.</p>
      )}
    </div>
  )
}

function routeKey(route: FactoryRoute): string {
  return `${route.connection}\u0000${route.match}`
}

/**
 * The Project's schedules: each with its next time, its last Run and a
 * button that pauses it at once (or continues it). Each member can pause.
 * There is no form to make a schedule: the CLI makes them.
 */
export function FactorySchedules({ schedules }: { schedules: FactorySchedule[] }) {
  const [changing, setChanging] = useState<string | null>(null)
  const change = (schedule: FactorySchedule) => {
    setChanging(schedule.id)
    router.post(schedule.paused ? schedule.continue_path : schedule.pause_path, {}, {
      preserveScroll: true,
      onFinish: () => setChanging(null),
    })
  }

  return (
    <div className="space-y-3" data-section="factory-schedules">
      <div>
        <h3 className="text-sm font-medium">Schedules</h3>
        <p className="text-xs text-muted-foreground">
          Runs that start by themselves. Make a schedule with <code className="font-mono">solidtrace schedules create</code>.
        </p>
      </div>
      {schedules.length === 0 ? (
        <p className="text-sm text-muted-foreground" data-testid="factory-no-schedules">No schedules.</p>
      ) : (
        <ul className="divide-y border">
          {schedules.map((schedule) => (
            <li key={schedule.id} className="flex flex-wrap items-center justify-between gap-3 px-3 py-2 text-sm" data-schedule={schedule.id}>
              <div className="min-w-0 space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{schedule.name}</span>
                  {schedule.paused && <Badge variant="warn">Paused</Badge>}
                </div>
                <div className="font-mono text-xs text-muted-foreground">
                  {schedule.cron} ({schedule.time_zone || 'UTC'})
                </div>
                <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                  <span data-field="next">Next: {scheduleNextText(schedule)}</span>
                  <span data-field="last-run" className="inline-flex items-center gap-1.5">
                    Last Run:{' '}
                    {schedule.last_run ? (
                      <>
                        <Link href={schedule.last_run.path} className="font-mono underline underline-offset-2" data-action="open-last-run">
                          {schedule.last_run.id}
                        </Link>
                        <Badge variant={stateTone(schedule.last_run.state)}>{stateText(schedule.last_run.state)}</Badge>
                      </>
                    ) : (
                      'none'
                    )}
                  </span>
                </div>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={changing !== null}
                onClick={() => change(schedule)}
                data-action={schedule.paused ? 'continue-schedule' : 'pause-schedule'}
              >
                {changing === schedule.id ? 'Saving…' : schedule.paused ? 'Continue' : 'Pause'}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
